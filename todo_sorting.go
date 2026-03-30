package main

import (
	"fmt"
	"math"
	"sort"
	"time"
)

type SortMode string

const (
	SortModePriority SortMode = "priority"
	SortModeDeadline SortMode = "deadline"
)

func sortModeLabel(mode SortMode) string {
	switch mode {
	case SortModeDeadline:
		return "\u622a\u6b62\u65f6\u95f4\u4f18\u5148"
	default:
		return "\u56db\u8c61\u9650\u4f18\u5148"
	}
}

func sortModeOptionLabel(mode SortMode) string {
	switch mode {
	case SortModeDeadline:
		return "\u6309 deadline \u6392\u5e8f"
	default:
		return "\u6309\u91cd\u8981 / \u7d27\u6025\u6392\u5e8f"
	}
}

func sortChevron(expanded bool) string {
	if expanded {
		return "\u25b2"
	}
	return "\u25be"
}

func sortVisibleTodos(todos []Todo, mode SortMode) []Todo {
	items := append([]Todo(nil), todos...)

	sort.SliceStable(items, func(i, j int) bool {
		leftDeadline := parseStoredTime(items[i].Deadline, time.Date(9999, 1, 1, 0, 0, 0, 0, time.UTC))
		rightDeadline := parseStoredTime(items[j].Deadline, time.Date(9999, 1, 1, 0, 0, 0, 0, time.UTC))
		leftCreated := parseStoredTime(items[i].CreatedAt, time.Time{})
		rightCreated := parseStoredTime(items[j].CreatedAt, time.Time{})
		leftRank := prioritySortRank(items[i].Kind)
		rightRank := prioritySortRank(items[j].Kind)

		switch mode {
		case SortModeDeadline:
			if !leftDeadline.Equal(rightDeadline) {
				return leftDeadline.Before(rightDeadline)
			}
			if leftRank != rightRank {
				return leftRank < rightRank
			}
		default:
			if leftRank != rightRank {
				return leftRank < rightRank
			}
			if !leftDeadline.Equal(rightDeadline) {
				return leftDeadline.Before(rightDeadline)
			}
		}

		return leftCreated.After(rightCreated)
	})

	return items
}

func prioritySortRank(kind TodoType) int {
	switch kind {
	case TodoUrgentImportant:
		return 0
	case TodoImportantNotUrgent:
		return 1
	case TodoUrgentNotImportant:
		return 2
	default:
		return 3
	}
}

func deadlineCountdownLabel(value string) string {
	deadline := parseStoredTime(value, time.Time{})
	if deadline.IsZero() {
		return "\u672a\u8bbe\u7f6e"
	}

	now := time.Now()
	diff := deadline.Sub(now)
	if diff <= 0 {
		past := -diff
		switch {
		case past < 90*time.Minute:
			return "\u521a\u5230\u671f"
		case past < 24*time.Hour:
			return fmt.Sprintf("\u5df2\u8fc7 %d \u5c0f\u65f6", int(math.Ceil(past.Hours())))
		case past < 45*24*time.Hour:
			return fmt.Sprintf("\u5df2\u8fc7 %d \u5929", int(math.Ceil(past.Hours()/24)))
		default:
			return fmt.Sprintf("\u5df2\u8fc7 %d \u6708", int(math.Ceil(past.Hours()/(24*30))))
		}
	}

	switch {
	case diff < time.Hour:
		return fmt.Sprintf("%d \u5206\u949f\u540e", maxInt(1, int(math.Ceil(diff.Minutes()))))
	case diff < 24*time.Hour:
		return fmt.Sprintf("%d \u5c0f\u65f6\u540e", int(math.Ceil(diff.Hours())))
	case diff < 45*24*time.Hour:
		return fmt.Sprintf("%d \u5929\u540e", int(math.Ceil(diff.Hours()/24)))
	case diff < 540*24*time.Hour:
		return fmt.Sprintf("%d \u6708\u540e", int(math.Ceil(diff.Hours()/(24*30))))
	default:
		return fmt.Sprintf("%d \u5e74\u540e", int(math.Ceil(diff.Hours()/(24*365))))
	}
}

func maxInt(left, right int) int {
	if left > right {
		return left
	}
	return right
}
