package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

type TodoType string

const (
	TodoUrgentImportant    TodoType = "紧急重要"
	TodoUrgentNotImportant TodoType = "紧急不重要"
	TodoImportantNotUrgent TodoType = "重要不紧急"
	TodoNeitherImportant   TodoType = "不重要不紧急"
)

var todoTypes = []TodoType{
	TodoUrgentImportant,
	TodoUrgentNotImportant,
	TodoImportantNotUrgent,
	TodoNeitherImportant,
}

type Todo struct {
	ID        string   `json:"id"`
	Title     string   `json:"title"`
	Details   string   `json:"details"`
	Progress  string   `json:"progress"`
	Tags      []string `json:"tags"`
	Deadline  string   `json:"deadline"`
	Kind      TodoType `json:"kind"`
	CreatedAt string   `json:"createdAt"`
	CompletedAt string `json:"completedAt"`
}

type TodoInput struct {
	Title    string `json:"title"`
	Details  string `json:"details"`
	Tags     string `json:"tags"`
	Deadline string `json:"deadline"`
	Kind     string `json:"kind"`
}

type AppSettings struct {
	UrgencyTintDays int  `json:"urgencyTintDays"`
	ReminderLeadMin int  `json:"reminderLeadMin"`
	AutoStart       bool `json:"autoStart"`
	AutoStartAsked  bool `json:"autoStartAsked"`
	BackgroundPath  string `json:"backgroundPath"`
	BackgroundAlpha int    `json:"backgroundAlpha"`
}

type storedData struct {
	Todos    []Todo      `json:"todos"`
	History  []Todo      `json:"history"`
	Settings AppSettings `json:"settings"`
}

type Store struct {
	mu   sync.Mutex
	path string
	data storedData
}

func AllTodoTypes() []TodoType {
	return append([]TodoType(nil), todoTypes...)
}

func NewStore() (*Store, error) {
	configDir, err := os.UserConfigDir()
	if err != nil {
		return nil, fmt.Errorf("获取配置目录失败: %w", err)
	}

	dir := filepath.Join(configDir, "todo-launcher")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("创建配置目录失败: %w", err)
	}

	store := &Store{
		path: filepath.Join(dir, "todos.json"),
		data: storedData{
			Todos:    []Todo{},
			History:  []Todo{},
			Settings: defaultAppSettings(),
		},
	}
	if err := store.load(); err != nil {
		return nil, err
	}

	return store, nil
}

func (s *Store) load() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	content, err := os.ReadFile(s.path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("读取待办数据失败: %w", err)
	}
	if len(content) == 0 {
		return nil
	}
	content = bytes.TrimPrefix(content, []byte{0xEF, 0xBB, 0xBF})

	if err := json.Unmarshal(content, &s.data); err != nil {
		return fmt.Errorf("解析待办数据失败: %w", err)
	}

	s.data.Settings = normalizeAppSettings(s.data.Settings)
	sortTodos(s.data.Todos)
	sortTodos(s.data.History)
	return nil
}

func (s *Store) List() []Todo {
	s.mu.Lock()
	defer s.mu.Unlock()

	return cloneTodos(s.data.Todos)
}

func (s *Store) HistoryList() []Todo {
	s.mu.Lock()
	defer s.mu.Unlock()

	return cloneTodos(s.data.History)
}

func (s *Store) Add(input TodoInput) ([]Todo, error) {
	title := strings.TrimSpace(input.Title)
	if title == "" {
		return nil, errors.New("请输入事件标题")
	}

	kind := TodoType(strings.TrimSpace(input.Kind))
	if !isValidTodoType(kind) {
		return nil, errors.New("请选择待办类型")
	}

	deadline, err := normalizeDeadline(input.Deadline)
	if err != nil {
		return nil, err
	}

	now := time.Now()
	todo := Todo{
		ID:        strconv.FormatInt(now.UnixNano(), 36),
		Title:     title,
		Details:   strings.TrimSpace(input.Details),
		Progress:  "",
		Tags:      normalizeTags(input.Tags),
		Deadline:  deadline.Format(time.RFC3339),
		Kind:      kind,
		CreatedAt: now.Format(time.RFC3339),
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	s.data.Todos = append(s.data.Todos, todo)
	sortTodos(s.data.Todos)

	if err := s.saveLocked(); err != nil {
		return nil, err
	}

	return cloneTodos(s.data.Todos), nil
}

func (s *Store) Settings() AppSettings {
	s.mu.Lock()
	defer s.mu.Unlock()

	return normalizeAppSettings(s.data.Settings)
}

func (s *Store) UpdateSettings(settings AppSettings) (AppSettings, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.data.Settings = normalizeAppSettings(settings)
	if err := s.saveLocked(); err != nil {
		return AppSettings{}, err
	}

	return s.data.Settings, nil
}

func (s *Store) Get(id string) (Todo, bool) {
	id = strings.TrimSpace(id)
	if id == "" {
		return Todo{}, false
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	for _, todo := range s.data.Todos {
		if todo.ID == id {
			return todo, true
		}
	}

	return Todo{}, false
}

func (s *Store) GetHistory(id string) (Todo, bool) {
	id = strings.TrimSpace(id)
	if id == "" {
		return Todo{}, false
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	for _, todo := range s.data.History {
		if todo.ID == id {
			return todo, true
		}
	}

	return Todo{}, false
}

func (s *Store) Remove(id string) ([]Todo, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return s.List(), nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	for i := range s.data.Todos {
		if s.data.Todos[i].ID != id {
			continue
		}

		s.data.Todos = append(s.data.Todos[:i], s.data.Todos[i+1:]...)
		if err := s.saveLocked(); err != nil {
			return nil, err
		}
		return cloneTodos(s.data.Todos), nil
	}

	return cloneTodos(s.data.Todos), nil
}

func (s *Store) RemoveHistory(id string) ([]Todo, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return s.HistoryList(), nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	for i := range s.data.History {
		if s.data.History[i].ID != id {
			continue
		}

		s.data.History = append(s.data.History[:i], s.data.History[i+1:]...)
		if err := s.saveLocked(); err != nil {
			return nil, err
		}
		return cloneTodos(s.data.History), nil
	}

	return cloneTodos(s.data.History), nil
}

func (s *Store) Complete(id string) ([]Todo, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return s.List(), nil
	}

	now := time.Now()

	s.mu.Lock()
	defer s.mu.Unlock()

	for i := range s.data.Todos {
		if s.data.Todos[i].ID != id {
			continue
		}

		done := s.data.Todos[i]
		done.CompletedAt = now.Format(time.RFC3339)
		s.data.Todos = append(s.data.Todos[:i], s.data.Todos[i+1:]...)
		s.data.History = append(s.data.History, done)
		sortTodos(s.data.Todos)
		sortTodos(s.data.History)
		if err := s.saveLocked(); err != nil {
			return nil, err
		}
		return cloneTodos(s.data.Todos), nil
	}

	return cloneTodos(s.data.Todos), nil
}

func (s *Store) UpdateProgress(id, progress string) ([]Todo, error) {
	return s.updateFields(id, nil, &progress, nil)
}

func (s *Store) AppendProgress(id, content string) ([]Todo, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return s.List(), nil
	}

	content = strings.TrimSpace(strings.ReplaceAll(content, "\r\n", "\n"))
	if content == "" {
		return s.List(), nil
	}

	entry := time.Now().Format("2006-01-02") + " " + content

	s.mu.Lock()
	defer s.mu.Unlock()

	for i := range s.data.Todos {
		if s.data.Todos[i].ID != id {
			continue
		}

		current := strings.TrimSpace(s.data.Todos[i].Progress)
		if current == "" {
			s.data.Todos[i].Progress = entry
		} else {
			s.data.Todos[i].Progress = current + "\n" + entry
		}
		if err := s.saveLocked(); err != nil {
			return nil, err
		}
		return cloneTodos(s.data.Todos), nil
	}

	return cloneTodos(s.data.Todos), nil
}

func (s *Store) UpdateDetailsAndTags(id, details, progress string, tags []string) ([]Todo, error) {
	return s.updateFields(id, &details, &progress, &tags)
}

func (s *Store) updateFields(id string, details *string, progress *string, tags *[]string) ([]Todo, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return s.List(), nil
	}

	if progress != nil {
		normalized := strings.ReplaceAll(*progress, "\r\n", "\n")
		progress = &normalized
	}
	if details != nil {
		normalized := strings.ReplaceAll(*details, "\r\n", "\n")
		details = &normalized
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	for i := range s.data.Todos {
		if s.data.Todos[i].ID != id {
			continue
		}

		if details != nil {
			s.data.Todos[i].Details = strings.TrimSpace(*details)
		}
		if progress != nil {
			s.data.Todos[i].Progress = *progress
		}
		if tags != nil {
			s.data.Todos[i].Tags = normalizeTags(strings.Join(*tags, ","))
		}
		if err := s.saveLocked(); err != nil {
			return nil, err
		}
		return cloneTodos(s.data.Todos), nil
	}

	return cloneTodos(s.data.Todos), nil
}

func (s *Store) UpdateDeadline(id, deadlineValue string) ([]Todo, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return s.List(), nil
	}

	deadline, err := normalizeDeadline(deadlineValue)
	if err != nil {
		return nil, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	for i := range s.data.Todos {
		if s.data.Todos[i].ID != id {
			continue
		}

		s.data.Todos[i].Deadline = deadline.Format(time.RFC3339)
		sortTodos(s.data.Todos)
		if err := s.saveLocked(); err != nil {
			return nil, err
		}
		return cloneTodos(s.data.Todos), nil
	}

	return cloneTodos(s.data.Todos), nil
}

func (s *Store) saveLocked() error {
	payload, err := json.MarshalIndent(s.data, "", "  ")
	if err != nil {
		return fmt.Errorf("序列化待办数据失败: %w", err)
	}

	if err := os.WriteFile(s.path, payload, 0o644); err != nil {
		return fmt.Errorf("保存待办数据失败: %w", err)
	}

	return nil
}

func cloneTodos(src []Todo) []Todo {
	items := append([]Todo(nil), src...)
	sortTodos(items)
	return items
}

func defaultAppSettings() AppSettings {
	return AppSettings{
		UrgencyTintDays: 15,
		ReminderLeadMin: 0,
		AutoStart:       false,
		AutoStartAsked:  false,
		BackgroundPath:  "",
		BackgroundAlpha: 72,
	}
}

func normalizeAppSettings(settings AppSettings) AppSettings {
	if settings.UrgencyTintDays <= 0 {
		settings.UrgencyTintDays = defaultAppSettings().UrgencyTintDays
	}
	if settings.UrgencyTintDays > 365 {
		settings.UrgencyTintDays = 365
	}
	if settings.ReminderLeadMin < 0 {
		settings.ReminderLeadMin = 0
	}
	if settings.ReminderLeadMin > 1440 {
		settings.ReminderLeadMin = 1440
	}
	settings.BackgroundPath = strings.TrimSpace(settings.BackgroundPath)
	if settings.BackgroundAlpha < 0 {
		settings.BackgroundAlpha = 0
	}
	if settings.BackgroundAlpha > 100 {
		settings.BackgroundAlpha = 100
	}
	return settings
}

func sortTodos(items []Todo) {
	sort.SliceStable(items, func(i, j int) bool {
		leftDeadline := parseStoredTime(items[i].Deadline, time.Date(9999, 1, 1, 0, 0, 0, 0, time.UTC))
		rightDeadline := parseStoredTime(items[j].Deadline, time.Date(9999, 1, 1, 0, 0, 0, 0, time.UTC))
		if !leftDeadline.Equal(rightDeadline) {
			return leftDeadline.Before(rightDeadline)
		}

		leftCreated := parseStoredTime(items[i].CreatedAt, time.Time{})
		rightCreated := parseStoredTime(items[j].CreatedAt, time.Time{})
		return leftCreated.After(rightCreated)
	})
}

func normalizeDeadline(value string) (time.Time, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}, errors.New("请选择 deadline")
	}

	layouts := []string{
		time.RFC3339,
		"2006-01-02T15:04",
		"2006-01-02 15:04",
	}
	for _, layout := range layouts {
		if t, err := time.ParseInLocation(layout, value, time.Local); err == nil {
			return t, nil
		}
	}

	return time.Time{}, errors.New("deadline 格式不正确")
}

func parseStoredTime(value string, fallback time.Time) time.Time {
	if value == "" {
		return fallback
	}
	if t, err := time.Parse(time.RFC3339, value); err == nil {
		return t
	}
	return fallback
}

func normalizeTags(input string) []string {
	input = strings.TrimSpace(input)
	if input == "" {
		return []string{}
	}

	parts := strings.FieldsFunc(input, func(r rune) bool {
		switch r {
		case ',', ';', '，', '、', '\n', '\t', ' ':
			return true
		default:
			return false
		}
	})

	out := make([]string, 0, len(parts))
	seen := make(map[string]struct{}, len(parts))
	for _, part := range parts {
		tag := strings.TrimSpace(part)
		if tag == "" {
			continue
		}
		key := strings.ToLower(tag)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, tag)
	}
	return out
}

func isValidTodoType(kind TodoType) bool {
	for _, item := range todoTypes {
		if item == kind {
			return true
		}
	}
	return false
}
