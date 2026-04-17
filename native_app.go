package main

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"syscall"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unsafe"

	"github.com/lxn/walk"
	. "github.com/lxn/walk/declarative"
	"github.com/lxn/win"
	"golang.org/x/sys/windows"
)

var (
	layeredUser32                  = windows.NewLazySystemDLL("user32.dll")
	procSetLayeredWindowAttributes = layeredUser32.NewProc("SetLayeredWindowAttributes")
	procSetWindowRgn               = layeredUser32.NewProc("SetWindowRgn")
	procSetWindowsHookExW          = layeredUser32.NewProc("SetWindowsHookExW")
	procUnhookWindowsHookEx        = layeredUser32.NewProc("UnhookWindowsHookEx")
	procCallNextHookEx             = layeredUser32.NewProc("CallNextHookEx")
	gdi32DLL                       = windows.NewLazySystemDLL("gdi32.dll")
	procCreateRoundRectRgn         = gdi32DLL.NewProc("CreateRoundRectRgn")
	mainWndProcPtr                 = syscall.NewCallback(mainWndProc)
	mouseHookProcPtr               = syscall.NewCallback(globalMouseHookProc)
	mainWndProcMu                  sync.Mutex
	mainWndProcMap                 = map[win.HWND]*NativeApp{}
	mouseHookMu                    sync.Mutex
	mouseHookApp                   *NativeApp
	wtsapi32DLL                    = windows.NewLazySystemDLL("wtsapi32.dll")
	procWTSRegisterSessionNotification   = wtsapi32DLL.NewProc("WTSRegisterSessionNotification")
	procWTSUnRegisterSessionNotification = wtsapi32DLL.NewProc("WTSUnRegisterSessionNotification")
)

const (
	layeredAlphaFlag   = 0x00000002
	overlayOpacity     = 250
	expandedWindowWidth = 480
	whMouseLL          = 14
	hcAction           = 0
	wmWTSSessionChange = 0x02B1
	wtsSessionUnlock   = 0x8
	notifThisSession   = 0
)

type msllhookstruct struct {
	Pt          win.POINT
	MouseData   uint32
	Flags       uint32
	Time        uint32
	DwExtraInfo uintptr
}

type NativeApp struct {
	store               *Store
	mw                  *walk.MainWindow
	headerHost          *walk.Composite
	footerHost          *walk.Composite
	dockHost            *walk.Composite
	headerWidget        *OverlayHeaderWidget
	collapseWidget      *OverlayCollapseWidget
	sortWidget          *OverlaySortWidget
	actionWidget        *OverlayActionWidget
	searchWidget        *OverlayMiniActionWidget
	dockWidget          *DockTabWidget
	listHost            *walk.Composite
	listWidget          *TodoBoardWidget
	rootLayout          *walk.BoxLayout
	painter             *CardPainter
	backgroundBitmap    *walk.Bitmap
	notifyIcon          *walk.NotifyIcon
	appIcon             *walk.Icon
	trayExpandAction    *walk.Action
	trayCollapseAction  *walk.Action
	traySettingsAction  *walk.Action
	trayHistoryAction   *walk.Action
	trayHistoryMenu     *walk.Menu
	windowWidth         int
	collapsedWidth      int
	collapsedHeight     int
	minWindowHeight     int
	maxWindowHeight     int
	maxHeightRatio      float64
	visible             bool
	collapsed           bool
	quitting            bool
	syncingWindowSize   bool
	deadlinePoll        time.Duration
	notificationStop    chan struct{}
	notified            map[string]bool
	reminded            map[string]bool
	reminderLead        time.Duration
	sortMode            SortMode
	sourceTodos         []Todo
	tagFilter           []string
	settings            AppSettings
	dialogDepth         int
	mainWndProcOrig     uintptr
	lastExpandedBounds  walk.Rectangle
	internalPointerActive bool
	mouseHook           uintptr
	processID           uint32
	trackedWindowsMu    sync.RWMutex
	trackedWindows      map[win.HWND]struct{}
	sessionNotifyRegistered bool
	lastUnlockSummaryDay    string
	summaryDialog           *walk.Dialog
}

func NewNativeApp(store *Store) (*NativeApp, error) {
	work := desktopWorkArea()
	settings := store.Settings()
	return &NativeApp{
		store:           store,
		windowWidth:     targetExpandedWindowWidth(work.Width),
		collapsedWidth:  60,
		collapsedHeight: 60,
		minWindowHeight: 0,
		maxWindowHeight: 1600,
		maxHeightRatio:  0.80,
		deadlinePoll:    15 * time.Second,
		notified:        map[string]bool{},
		reminded:        map[string]bool{},
		sortMode:        SortModePriority,
		settings:        settings,
		trackedWindows:  map[win.HWND]struct{}{},
	}, nil
}

func (a *NativeApp) Run() error {
	if err := a.buildUI(); err != nil {
		return err
	}
	if err := a.setupNotifyIcon(); err != nil {
		return err
	}

	painter, err := NewCardPainter(a.mw)
	if err != nil {
		return err
	}
	a.painter = painter
	a.applySettings(a.settings)

	listWidget, err := NewTodoBoardWidget(a.listHost, painter, a.completeTodo, a.openPostponeDialog, a.openDetailDialog, "\u63a8\u8fdf", true, true, true)
	if err != nil {
		return err
	}
	a.listWidget = listWidget
	a.listWidget.SetWidthHint(a.boardWidthHint())
	a.listWidget.SetSkin(a.backgroundBitmap, a.settings.BackgroundAlpha)
	if a.listHost != nil {
		resizeListViewport := func() {
			if a.listWidget == nil || a.listHost == nil {
				return
			}
			bounds := a.listHost.ClientBoundsPixels()
			if bounds.Width <= 0 || bounds.Height <= 0 {
				return
			}
			a.listWidget.SetWidthHint(a.boardWidthHint())
			_ = a.listWidget.SetBoundsPixels(bounds)
		}
		a.listHost.SizeChanged().Attach(func() {
			resizeListViewport()
		})
		a.mw.SizeChanged().Attach(func() {
			resizeListViewport()
		})
	}

	a.applyTodos(a.store.List())
	a.startDeadlineWatcher()
	a.primeWindowState()
	a.setWindowCollapsed(true, false)
	a.showWindow()
	if err := a.installGlobalMouseHook(); err != nil {
		return err
	}
	a.mw.Run()

	return nil
}

func (a *NativeApp) SummonFromHotkey() {
	if a.mw == nil {
		return
	}

	a.mw.Synchronize(func() {
		if !a.visible || a.collapsed || win.IsIconic(a.mw.Handle()) {
			a.showWindow()
			return
		}
		a.collapseWindow()
	})
}

func (a *NativeApp) buildUI() error {
	if err := (MainWindow{
		AssignTo: &a.mw,
		Title:    "todoooo",
		MinSize:  Size{Width: a.collapsedWidth, Height: a.collapsedHeight},
		MaxSize:  Size{Width: a.windowWidth, Height: a.maxWindowHeight},
		OnKeyDown: func(key walk.Key) {
			if key == walk.KeyEscape {
				a.collapseWindow()
			}
		},
		Background: SolidColorBrush{Color: rgb(245, 247, 251)},
		Layout: VBox{
			Margins: Margins{Left: 14, Top: 16, Right: 14, Bottom: 16},
			Spacing: 12,
		},
		Font: Font{Family: "Microsoft YaHei UI", PointSize: 10},
		Children: []Widget{
			Composite{
				AssignTo:   &a.headerHost,
				Background: SolidColorBrush{Color: rgb(245, 247, 251)},
				Layout: VBox{
					MarginsZero: true,
					Spacing:     10,
				},
			},
			Composite{
				AssignTo:       &a.listHost,
				StretchFactor:  1,
				Background:     SolidColorBrush{Color: boardBackgroundColor()},
				Layout: VBox{
					MarginsZero: true,
					SpacingZero: true,
					Alignment:   AlignHNearVNear,
				},
			},
			Composite{
				AssignTo:   &a.footerHost,
				Background: SolidColorBrush{Color: rgb(245, 247, 251)},
				Layout: VBox{
					MarginsZero: true,
					SpacingZero: true,
				},
			},
			Composite{
				AssignTo:   &a.dockHost,
				Visible:    false,
				Background: SolidColorBrush{Color: rgb(245, 247, 251)},
				Layout: VBox{
					MarginsZero: true,
					SpacingZero: true,
				},
			},
		},
	}).Create(); err != nil {
		return err
	}

	a.installMainWndProc()
	a.registerTrackedWindow(a.mw.Handle())
	a.registerSessionNotifications()
	_ = a.mw.SetDoubleBuffering(true)
	if a.listHost != nil {
		_ = a.listHost.SetDoubleBuffering(true)
	}
	if layout, ok := a.mw.Layout().(*walk.BoxLayout); ok {
		a.rootLayout = layout
	}
	a.applyRootLayoutState(false)
	applyMainWindowChrome(a.mw.Handle(), false)
	a.mw.Closing().Attach(func(canceled *bool, reason walk.CloseReason) {
		if a.quitting {
			return
		}
		*canceled = true
		a.collapseWindow()
	})
	a.mw.Deactivating().Attach(func() {
		// Collapse is driven only by global mouse-down outside app windows.
	})
	a.mw.MouseUp().Attach(func(x, y int, button walk.MouseButton) {
		if button == walk.LeftButton && a.collapsed {
			a.showWindow()
		}
	})

	headerRow, err := walk.NewComposite(a.headerHost)
	if err != nil {
		return err
	}
	headerRow.SetBackground(walk.NullBrush())
	headerLayout := walk.NewHBoxLayout()
	_ = headerLayout.SetMargins(walk.Margins{})
	_ = headerLayout.SetSpacing(10)
	if err := headerRow.SetLayout(headerLayout); err != nil {
		return err
	}

	headerWidget, err := NewOverlayHeaderWidget(
		headerRow,
		"todoooo",
		"Ctrl + \u5c0f\u952e\u76d8 9 \u5c55\u5f00 / \u6536\u8d77\uff1b\u53f3\u952e\u5f85\u529e\u53ef\u67e5\u770b\u8be6\u60c5\u5e76\u66f4\u65b0\u8fdb\u5c55\u3002",
		func() {
			a.beginInternalPointerAction()
			startWindowDrag(a.mw.Handle())
		},
	)
	if err != nil {
		return err
	}
	a.headerWidget = headerWidget

	collapseWidget, err := NewOverlayCollapseWidget(headerRow, func() {
		a.collapseWindow()
	})
	if err != nil {
		return err
	}
	a.collapseWidget = collapseWidget

	sortWidget, err := NewOverlaySortWidget(a.headerHost, a.sortMode, func(mode SortMode) {
		a.setSortMode(mode)
	}, func() {
		a.syncWindowSize()
	})
	if err != nil {
		return err
	}
	a.sortWidget = sortWidget

	footerRow, err := walk.NewComposite(a.footerHost)
	if err != nil {
		return err
	}
	footerRow.SetBackground(walk.NullBrush())
	footerLayout := walk.NewHBoxLayout()
	_ = footerLayout.SetMargins(walk.Margins{})
	_ = footerLayout.SetSpacing(10)
	if err := footerRow.SetLayout(footerLayout); err != nil {
		return err
	}

	actionWidget, err := NewOverlayActionWidget(footerRow, "+ \u65b0\u589e\u4e8b\u9879", func() {
		a.openAddDialog()
	})
	if err != nil {
		return err
	}
	a.actionWidget = actionWidget

	searchWidget, err := NewOverlayMiniActionWidget(footerRow, "\u6807\u7b7e\u641c\u7d22", func() {
		a.openTagSearchDialog()
	})
	if err != nil {
		return err
	}
	a.searchWidget = searchWidget

	dockWidget, err := NewDockTabWidget(a.dockHost, func() {
		a.showWindow()
	})
	if err != nil {
		return err
	}
	a.dockWidget = dockWidget

	return nil
}

func (a *NativeApp) applyTodos(todos []Todo) {
	a.sourceTodos = append([]Todo(nil), todos...)

	if a.headerWidget != nil {
		a.headerWidget.SetCountText(len(todos))
	}
	if a.dockWidget != nil {
		a.dockWidget.SetCounts(len(todos), todoKindCounts(todos))
	}
	if a.sortWidget != nil {
		a.sortWidget.SetSelected(a.sortMode)
	}
	if a.notifyIcon != nil {
		_ = a.notifyIcon.SetToolTip(fmt.Sprintf("todoooo - %d \u9879\u5f85\u529e", len(todos)))
	}

	if a.listWidget != nil {
		viewTodos := sortVisibleTodos(todos, a.sortMode)
		viewTodos = filterTodosByTags(viewTodos, a.tagFilter)
		a.listWidget.SetWidthHint(a.boardWidthHint())
		a.listWidget.SetTodos(viewTodos)
	}
	a.updateSearchLabel()
	a.syncNotificationMaps(todos)

	a.syncWindowSize()
	a.refreshHistoryMenu()
}

func (a *NativeApp) completeTodo(id string) {
	todos, err := a.store.Complete(id)
	if err != nil {
		a.showError("\u79fb\u9664\u5931\u8d25", err)
		return
	}

	a.applyTodos(todos)
	a.refreshHistoryMenu()
}

func (a *NativeApp) openAddDialog() {
	if a.mw == nil {
		return
	}
	a.beginDialog()
	defer a.endDialog()

	var dlg *walk.Dialog
	var headerHost *walk.Composite
	var cancelHost *walk.Composite
	var saveHost *walk.Composite
	var titleEdit *walk.LineEdit
	var detailsEdit *walk.TextEdit
	var deadlineEdit *walk.DateEdit
	var deadlineHourBox *walk.ComboBox
	var deadlineMinuteBox *walk.ComboBox
	var kindBox *walk.ComboBox
	var tagsEdit *walk.LineEdit
	var errLabel *walk.Label

	todoTypes := make([]string, 0, len(AllTodoTypes()))
	for _, item := range AllTodoTypes() {
		todoTypes = append(todoTypes, string(item))
	}

	defaultDeadline := time.Now().Add(time.Hour).Truncate(time.Minute)
	deadlineHours, deadlineMinutes := deadlineSelectorOptions()

	buildDeadlineValue := func() (string, error) {
		date := deadlineEdit.Date()
		if date.IsZero() {
			return "", fmt.Errorf("请选择截止日期")
		}

		hour, err := strconv.Atoi(deadlineHourBox.Text())
		if err != nil {
			return "", fmt.Errorf("请选择截止小时")
		}
		minute, err := strconv.Atoi(deadlineMinuteBox.Text())
		if err != nil {
			return "", fmt.Errorf("请选择截止分钟")
		}

		deadline := time.Date(date.Year(), date.Month(), date.Day(), hour, minute, 0, 0, time.Local)
		return deadline.Format("2006-01-02 15:04"), nil
	}

	saveTodo := func() {
		deadlineValue, err := buildDeadlineValue()
		if err != nil {
			errLabel.SetText(err.Error())
			return
		}

		input := TodoInput{
			Title:    titleEdit.Text(),
			Details:  detailsEdit.Text(),
			Tags:     tagsEdit.Text(),
			Deadline: deadlineValue,
			Kind:     kindBox.Text(),
		}

		errLabel.SetText("")
		todos, saveErr := a.store.Add(input)
		if saveErr != nil {
			errLabel.SetText(saveErr.Error())
			return
		}

		a.applyTodos(todos)
		dlg.Accept()
	}

	if err := (Dialog{
		AssignTo:  &dlg,
		Title:     "新增待办",
		MinSize:   Size{Width: 520, Height: 600},
		Background: SolidColorBrush{
			Color: boardBackgroundColor(),
		},
		Layout: VBox{
			Margins: Margins{Left: 18, Top: 18, Right: 18, Bottom: 18},
			Spacing: 12,
		},
		Font: Font{Family: "Microsoft YaHei UI", PointSize: 10},
		OnKeyDown: func(key walk.Key) {
			if key == walk.KeyEscape {
				dlg.Cancel()
			}
		},
		Children: []Widget{
			Composite{
				AssignTo:   &headerHost,
				Background: SolidColorBrush{Color: boardBackgroundColor()},
				Layout: VBox{
					MarginsZero: true,
					SpacingZero: true,
				},
			},
			Composite{
				Background: SolidColorBrush{Color: rgb(252, 254, 255)},
				Layout: VBox{
					Margins: Margins{Left: 16, Top: 16, Right: 16, Bottom: 16},
					Spacing: 10,
				},
				Children: []Widget{
					Label{
						Text:      "填写标题、详情、截止时间和象限后保存。",
						TextColor: rgb(95, 103, 112),
					},
					Label{Text: "事项标题", TextColor: rgb(76, 88, 100)},
					LineEdit{
						AssignTo:   &titleEdit,
						Background: SolidColorBrush{Color: rgb(247, 250, 253)},
					},
					Label{Text: "详情内容", TextColor: rgb(76, 88, 100)},
					TextEdit{
						AssignTo:      &detailsEdit,
						Background:    SolidColorBrush{Color: rgb(247, 250, 253)},
						StretchFactor: 1,
						MinSize:       Size{Height: 180},
						VScroll:       true,
					},
					Label{Text: "\u6807\u7b7e\uff08\u9017\u53f7\u5206\u9694\uff09", TextColor: rgb(76, 88, 100)},
					LineEdit{
						AssignTo:   &tagsEdit,
						Background: SolidColorBrush{Color: rgb(247, 250, 253)},
					},
					Composite{
						Background: SolidColorBrush{Color: rgb(252, 254, 255)},
						Layout: Grid{
							Columns: 2,
							Margins: Margins{},
							Spacing: 10,
						},
						Children: []Widget{
							Label{Text: "截止时间", TextColor: rgb(76, 88, 100)},
							Label{Text: "待办类型", TextColor: rgb(76, 88, 100)},
							Composite{
								Background: SolidColorBrush{Color: rgb(252, 254, 255)},
								Layout: HBox{
									MarginsZero: true,
									Spacing:     8,
								},
								Children: []Widget{
									DateEdit{
										AssignTo:      &deadlineEdit,
										Background:    SolidColorBrush{Color: rgb(247, 250, 253)},
										Date:          defaultDeadline,
										Format:        "yyyy-MM-dd",
										StretchFactor: 1,
									},
									ComboBox{
										AssignTo:   &deadlineHourBox,
										Background: SolidColorBrush{Color: rgb(247, 250, 253)},
										Model:      deadlineHours,
										MaxSize:    Size{Width: 68},
										MinSize:    Size{Width: 68},
									},
									Label{
										Text:      ":",
										TextColor: rgb(112, 121, 130),
									},
									ComboBox{
										AssignTo:   &deadlineMinuteBox,
										Background: SolidColorBrush{Color: rgb(247, 250, 253)},
										Model:      deadlineMinutes,
										MaxSize:    Size{Width: 68},
										MinSize:    Size{Width: 68},
									},
								},
							},
							ComboBox{
								AssignTo:   &kindBox,
								Background: SolidColorBrush{Color: rgb(247, 250, 253)},
								Model:      todoTypes,
							},
						},
					},
					Label{
						AssignTo:  &errLabel,
						TextColor: rgb(184, 56, 43),
					},
				},
			},
			Composite{
				Background: SolidColorBrush{Color: rgb(252, 254, 255)},
				Layout: HBox{
					Margins: Margins{Left: 16, Top: 14, Right: 16, Bottom: 14},
					Spacing: 10,
				},
				Children: []Widget{
					HSpacer{},
					Composite{
						AssignTo:   &cancelHost,
						Background: SolidColorBrush{Color: rgb(252, 254, 255)},
						Layout: VBox{
							MarginsZero: true,
							SpacingZero: true,
						},
					},
					Composite{
						AssignTo:   &saveHost,
						Background: SolidColorBrush{Color: rgb(252, 254, 255)},
						Layout: VBox{
							MarginsZero: true,
							SpacingZero: true,
						},
					},
				},
			},
		},
	}).Create(a.mw); err != nil {
		a.showError("打开新增窗口失败", err)
		return
	}

	prepareOverlayDialog(dlg)
	a.trackDialogWindow(dlg)
	if _, err := NewOverlayDialogHeaderWidget(headerHost, "新增事项", "填写完成后保存到主面板列表。", func() {
		startWindowDrag(dlg.Handle())
	}); err != nil {
		dlg.Dispose()
		a.showError("创建新增窗口头部失败", err)
		return
	}
	if _, err := NewOverlayDialogActionWidget(cancelHost, "取消", false, func() {
		dlg.Cancel()
	}); err != nil {
		dlg.Dispose()
		a.showError("创建新增窗口按钮失败", err)
		return
	}
	if _, err := NewOverlayDialogActionWidget(saveHost, "保存事项", true, func() {
		saveTodo()
	}); err != nil {
		dlg.Dispose()
		a.showError("创建新增窗口按钮失败", err)
		return
	}

	if len(todoTypes) > 0 {
		_ = kindBox.SetCurrentIndex(0)
	}
	_ = deadlineHourBox.SetCurrentIndex(defaultDeadline.Hour())
	_ = deadlineMinuteBox.SetCurrentIndex(defaultDeadline.Minute())

	titleEdit.SetFocus()
	if dlg.Run() == walk.DlgCmdOK {
		a.showWindow()
	}
	a.ensureCollapsedVisible()
}

func (a *NativeApp) openPostponeDialog(id string) {
	if a.mw == nil {
		return
	}
	a.beginDialog()
	defer a.endDialog()

	todo, ok := a.store.Get(id)
	if !ok {
		walk.MsgBox(a.mw, "打开推迟失败", "没有找到对应的待办。", walk.MsgBoxOK|walk.MsgBoxIconWarning)
		return
	}

	var dlg *walk.Dialog
	var headerHost *walk.Composite
	var cancelHost *walk.Composite
	var saveHost *walk.Composite
	var deadlineEdit *walk.DateEdit
	var deadlineHourBox *walk.ComboBox
	var deadlineMinuteBox *walk.ComboBox
	var errLabel *walk.Label

	deadlineHours, deadlineMinutes := deadlineSelectorOptions()
	defaultDeadline := parseStoredTime(todo.Deadline, time.Now().Add(time.Hour).Truncate(time.Minute)).Local().Truncate(time.Minute)
	if defaultDeadline.IsZero() {
		defaultDeadline = time.Now().Add(time.Hour).Truncate(time.Minute)
	}

	saveDeadline := func() {
		deadlineValue, err := composeDeadlineValue(deadlineEdit.Date(), deadlineHourBox.Text(), deadlineMinuteBox.Text())
		if err != nil {
			errLabel.SetText(err.Error())
			return
		}

		todos, saveErr := a.store.UpdateDeadline(id, deadlineValue)
		if saveErr != nil {
			errLabel.SetText(saveErr.Error())
			return
		}

		errLabel.SetText("")
		a.applyTodos(todos)
		dlg.Accept()
	}

	if err := (Dialog{
		AssignTo:  &dlg,
		Title:     "推迟待办",
		FixedSize: true,
		MinSize:   Size{Width: 500, Height: 330},
		MaxSize:   Size{Width: 500, Height: 330},
		Background: SolidColorBrush{
			Color: boardBackgroundColor(),
		},
		Layout: VBox{
			Margins: Margins{Left: 18, Top: 18, Right: 18, Bottom: 18},
			Spacing: 12,
		},
		Font: Font{Family: "Microsoft YaHei UI", PointSize: 10},
		OnKeyDown: func(key walk.Key) {
			if key == walk.KeyEscape {
				dlg.Cancel()
			}
		},
		Children: []Widget{
			Composite{
				AssignTo:   &headerHost,
				Background: SolidColorBrush{Color: boardBackgroundColor()},
				Layout: VBox{
					MarginsZero: true,
					SpacingZero: true,
				},
			},
			Composite{
				Background: SolidColorBrush{Color: rgb(252, 254, 255)},
				Layout: VBox{
					Margins: Margins{Left: 16, Top: 16, Right: 16, Bottom: 16},
					Spacing: 10,
				},
				Children: []Widget{
					Label{
						Text:      todo.Title,
						Font:      Font{Family: "Microsoft YaHei UI", PointSize: 12, Bold: true},
						TextColor: rgb(34, 42, 50),
					},
					Label{
						Text:      "选择新的截止时间，保存后会立即更新排序和倒计时。",
						TextColor: rgb(95, 103, 112),
					},
					Label{Text: "新的截止时间", TextColor: rgb(76, 88, 100)},
					Composite{
						Background: SolidColorBrush{Color: rgb(252, 254, 255)},
						Layout: HBox{
							MarginsZero: true,
							Spacing:     8,
						},
						Children: []Widget{
							DateEdit{
								AssignTo:      &deadlineEdit,
								Background:    SolidColorBrush{Color: rgb(247, 250, 253)},
								Date:          defaultDeadline,
								Format:        "yyyy-MM-dd",
								StretchFactor: 1,
							},
							ComboBox{
								AssignTo:   &deadlineHourBox,
								Background: SolidColorBrush{Color: rgb(247, 250, 253)},
								Model:      deadlineHours,
								MaxSize:    Size{Width: 68},
								MinSize:    Size{Width: 68},
							},
							Label{
								Text:      ":",
								TextColor: rgb(112, 121, 130),
							},
							ComboBox{
								AssignTo:   &deadlineMinuteBox,
								Background: SolidColorBrush{Color: rgb(247, 250, 253)},
								Model:      deadlineMinutes,
								MaxSize:    Size{Width: 68},
								MinSize:    Size{Width: 68},
							},
						},
					},
					Label{
						AssignTo:  &errLabel,
						TextColor: rgb(184, 56, 43),
					},
				},
			},
			Composite{
				Background: SolidColorBrush{Color: rgb(252, 254, 255)},
				Layout: HBox{
					Margins: Margins{Left: 16, Top: 14, Right: 16, Bottom: 14},
					Spacing: 10,
				},
				Children: []Widget{
					HSpacer{},
					Composite{
						AssignTo:   &cancelHost,
						Background: SolidColorBrush{Color: rgb(252, 254, 255)},
						Layout: VBox{
							MarginsZero: true,
							SpacingZero: true,
						},
					},
					Composite{
						AssignTo:   &saveHost,
						Background: SolidColorBrush{Color: rgb(252, 254, 255)},
						Layout: VBox{
							MarginsZero: true,
							SpacingZero: true,
						},
					},
				},
			},
		},
	}).Create(a.mw); err != nil {
		a.showError("打开推迟窗口失败", err)
		return
	}

	prepareOverlayDialog(dlg)
	a.trackDialogWindow(dlg)
	if _, err := NewOverlayDialogHeaderWidget(headerHost, "推迟待办", "只修改 deadline，不影响事项内容和进展。", func() {
		startWindowDrag(dlg.Handle())
	}); err != nil {
		dlg.Dispose()
		a.showError("创建推迟窗口头部失败", err)
		return
	}
	if _, err := NewOverlayDialogActionWidget(cancelHost, "取消", false, func() {
		dlg.Cancel()
	}); err != nil {
		dlg.Dispose()
		a.showError("创建推迟窗口按钮失败", err)
		return
	}
	if _, err := NewOverlayDialogActionWidget(saveHost, "保存时间", true, func() {
		saveDeadline()
	}); err != nil {
		dlg.Dispose()
		a.showError("创建推迟窗口按钮失败", err)
		return
	}

	_ = deadlineHourBox.SetCurrentIndex(defaultDeadline.Hour())
	_ = deadlineMinuteBox.SetCurrentIndex(defaultDeadline.Minute())
	dlg.Run()
	a.ensureCollapsedVisible()
}

func (a *NativeApp) openDetailDialog(id string) {
	if a.mw == nil {
		return
	}
	a.beginDialog()
	defer a.endDialog()

	todo, ok := a.store.Get(id)
	if !ok {
		walk.MsgBox(a.mw, "打开详情失败", "没有找到对应的待办。", walk.MsgBoxOK|walk.MsgBoxIconWarning)
		return
	}

	var dlg *walk.Dialog
	var headerHost *walk.Composite
	var closeHost *walk.Composite
	var detailsEdit *walk.TextEdit
	var progressListEdit *walk.TextEdit
	var progressInputEdit *walk.TextEdit
	var progressAddHost *walk.Composite
	var tagsEdit *walk.LineEdit
	var statusLabel *walk.Label

	dirty := false
	lastSavedDetails := normalizeProgressValue(todo.Details)
	lastSavedTagsKey := tagsKey(todo.Tags)
	const autoSaveDelay = 700 * time.Millisecond

	var saveMu sync.Mutex
	var saveTimerMu sync.Mutex
	var saveTimer *time.Timer

	stopScheduledSave := func() {
		saveTimerMu.Lock()
		defer saveTimerMu.Unlock()
		if saveTimer == nil {
			return
		}
		saveTimer.Stop()
		saveTimer = nil
	}

	setStatus := func(text string, color walk.Color) {
		if statusLabel == nil || statusLabel.IsDisposed() {
			return
		}
		statusLabel.SetText(text)
		statusLabel.SetTextColor(color)
	}

	saveProgress := func(force bool) error {
		saveMu.Lock()
		defer saveMu.Unlock()
		if detailsEdit == nil || detailsEdit.IsDisposed() || tagsEdit == nil || tagsEdit.IsDisposed() {
			return nil
		}

		details := normalizeProgressValue(detailsEdit.Text())
		tags := normalizeTags(tagsEdit.Text())
		tagKey := tagsKey(tags)

		if !force && !dirty && details == lastSavedDetails && tagKey == lastSavedTagsKey {
			return nil
		}
		if details == lastSavedDetails && tagKey == lastSavedTagsKey {
			dirty = false
			setStatus("内容与已保存版本一致", rgb(98, 107, 116))
			return nil
		}

		todos, err := a.store.UpdateDetailsAndTags(id, details, todo.Progress, tags)
		if err != nil {
			return err
		}

		lastSavedDetails = details
		lastSavedTagsKey = tagKey
		dirty = false
		a.applyTodos(todos)
		setStatus("已自动保存 "+time.Now().Format("15:04:05"), rgb(45, 117, 88))
		return nil
	}

	scheduleSave := func() {
		saveTimerMu.Lock()
		defer saveTimerMu.Unlock()
		if saveTimer != nil {
			saveTimer.Stop()
		}
		saveTimer = time.AfterFunc(autoSaveDelay, func() {
			if dlg == nil || dlg.IsDisposed() {
				return
			}
			dlg.Synchronize(func() {
				if dlg == nil || dlg.IsDisposed() {
					return
				}
				if err := saveProgress(false); err != nil {
					setStatus("自动保存失败，请关闭前重试", rgb(184, 56, 43))
				}
			})
		})
	}

	appendProgress := func() {
		if progressInputEdit == nil || progressInputEdit.IsDisposed() {
			return
		}
		content := strings.TrimSpace(progressInputEdit.Text())
		if content == "" {
			setStatus("请输入进展内容", rgb(184, 56, 43))
			return
		}
		todos, err := a.store.AppendProgress(id, content)
		if err != nil {
			setStatus("添加进展失败，请重试", rgb(184, 56, 43))
			return
		}
		progressInputEdit.SetText("")
		if updated, ok := a.store.Get(id); ok {
			todo = updated
		}
		if progressListEdit != nil && !progressListEdit.IsDisposed() {
			progressListEdit.SetText(progressTimeline(todo.Progress))
		}
		a.applyTodos(todos)
		setStatus("已新增一条进展", rgb(45, 117, 88))
	}

	if err := (Dialog{
		AssignTo: &dlg,
		Title:    "待办详情",
		MinSize:  Size{Width: 620, Height: 680},
		Background: SolidColorBrush{
			Color: boardBackgroundColor(),
		},
		Layout: VBox{
			Margins: Margins{Left: 18, Top: 18, Right: 18, Bottom: 18},
			Spacing: 12,
		},
		Font: Font{Family: "Microsoft YaHei UI", PointSize: 10},
		OnKeyDown: func(key walk.Key) {
			if key == walk.KeyEscape {
				dlg.Cancel()
			}
		},
		Children: []Widget{
			Composite{
				AssignTo:   &headerHost,
				Background: SolidColorBrush{Color: boardBackgroundColor()},
				Layout: VBox{
					MarginsZero: true,
					SpacingZero: true,
				},
			},
			Composite{
				Background: SolidColorBrush{Color: rgb(252, 254, 255)},
				Layout: VBox{
					Margins: Margins{Left: 16, Top: 16, Right: 16, Bottom: 16},
					Spacing: 10,
				},
				Children: []Widget{
					Label{
						Text:      todo.Title,
						Font:      Font{Family: "Microsoft YaHei UI", PointSize: 14, Bold: true},
						TextColor: rgb(28, 34, 42),
					},
					Composite{
						Background: SolidColorBrush{Color: rgb(252, 254, 255)},
						Layout: Grid{
							Columns: 2,
							Margins: Margins{},
							Spacing: 8,
						},
						Children: []Widget{
							Label{Text: "待办类型", TextColor: rgb(98, 107, 116)},
							Label{Text: "截止时间", TextColor: rgb(98, 107, 116)},
							Label{Text: string(todo.Kind)},
							Label{Text: formatTodoDeadline(todo.Deadline)},
						},
					},
				},
			},
			Composite{
				Background: SolidColorBrush{Color: rgb(252, 254, 255)},
				Layout: VBox{
					Margins: Margins{Left: 16, Top: 16, Right: 16, Bottom: 16},
					Spacing: 8,
				},
				Children: []Widget{
					Label{
						Text:      "详情内容",
						TextColor: rgb(98, 107, 116),
					},
					TextEdit{
						AssignTo:      &detailsEdit,
						Text:          todo.Details,
						Background:    SolidColorBrush{Color: rgb(247, 250, 253)},
						StretchFactor: 1,
						VScroll:       true,
						MinSize:       Size{Height: 72},
					},
				},
			},
			Composite{
				Background: SolidColorBrush{Color: rgb(252, 254, 255)},
				Layout: VBox{
					Margins: Margins{Left: 16, Top: 16, Right: 16, Bottom: 16},
					Spacing: 8,
				},
				Children: []Widget{
					Label{
						Text:      "标签（逗号分隔）",
						TextColor: rgb(98, 107, 116),
					},
					LineEdit{
						AssignTo:   &tagsEdit,
						Text:       strings.Join(todo.Tags, ", "),
						Background: SolidColorBrush{Color: rgb(247, 250, 253)},
					},
				},
			},
			Composite{
				Background: SolidColorBrush{Color: rgb(252, 254, 255)},
				Layout: VBox{
					Margins: Margins{Left: 16, Top: 16, Right: 16, Bottom: 16},
					Spacing: 8,
				},
				Children: []Widget{
					Label{
						Text:      "实时进展",
						TextColor: rgb(98, 107, 116),
					},
					TextEdit{
						AssignTo:      &progressListEdit,
						Text:          progressTimeline(todo.Progress),
						Background:    SolidColorBrush{Color: rgb(247, 250, 253)},
						ReadOnly:      true,
						StretchFactor: 1,
						VScroll:       true,
						MinSize:       Size{Height: 170},
					},
					Label{
						Text:      "新增进展",
						TextColor: rgb(98, 107, 116),
					},
					TextEdit{
						AssignTo:      &progressInputEdit,
						Background:    SolidColorBrush{Color: rgb(247, 250, 253)},
						StretchFactor: 1,
						VScroll:       true,
						MinSize:       Size{Height: 140},
					},
					Composite{
						AssignTo:   &progressAddHost,
						Background: SolidColorBrush{Color: rgb(252, 254, 255)},
						Layout: VBox{
							MarginsZero: true,
							SpacingZero: true,
						},
					},
					Label{
						AssignTo:  &statusLabel,
						Text:      "详情和标签会自动保存；新增进展后会按日期追加一行",
						TextColor: rgb(98, 107, 116),
					},
				},
			},
			Composite{
				Background: SolidColorBrush{Color: rgb(252, 254, 255)},
				Layout: HBox{
					Margins: Margins{Left: 16, Top: 14, Right: 16, Bottom: 14},
					Spacing: 10,
				},
				Children: []Widget{
					HSpacer{},
					Composite{
						AssignTo:   &closeHost,
						Background: SolidColorBrush{Color: rgb(252, 254, 255)},
						Layout: VBox{
							MarginsZero: true,
							SpacingZero: true,
						},
					},
				},
			},
		},
	}).Create(a.mw); err != nil {
		a.showError("打开详情失败", err)
		return
	}

	prepareOverlayDialog(dlg)
	a.trackDialogWindow(dlg)
	if _, err := NewOverlayDialogHeaderWidget(headerHost, "待办详情", "查看内容并记录实时进展。", func() {
		startWindowDrag(dlg.Handle())
	}); err != nil {
		dlg.Dispose()
		a.showError("创建详情窗口头部失败", err)
		return
	}
	if _, err := NewOverlayDialogActionWidget(closeHost, "关闭", true, func() {
		stopScheduledSave()
		if err := saveProgress(true); err != nil {
			setStatus("保存失败，请重试", rgb(184, 56, 43))
			walk.MsgBox(dlg, "保存进展失败", err.Error(), walk.MsgBoxOK|walk.MsgBoxIconError)
			return
		}
		dlg.Accept()
	}); err != nil {
		dlg.Dispose()
		a.showError("创建详情窗口按钮失败", err)
		return
	}
	if _, err := NewOverlayDialogActionWidget(progressAddHost, "新增进展", false, func() {
		appendProgress()
	}); err != nil {
		dlg.Dispose()
		a.showError("创建详情窗口按钮失败", err)
		return
	}

	detailsEdit.TextChanged().Attach(func() {
		currentDetails := normalizeProgressValue(detailsEdit.Text())
		dirty = currentDetails != lastSavedDetails || tagsKey(normalizeTags(tagsEdit.Text())) != lastSavedTagsKey
		if dirty {
			setStatus("编辑中，稍后自动保存", rgb(98, 107, 116))
			scheduleSave()
			return
		}
		stopScheduledSave()
		setStatus("内容与已保存版本一致", rgb(98, 107, 116))
	})
	tagsEdit.TextChanged().Attach(func() {
		dirty = tagsKey(normalizeTags(tagsEdit.Text())) != lastSavedTagsKey || normalizeProgressValue(detailsEdit.Text()) != lastSavedDetails
		if dirty {
			setStatus("编辑中，稍后自动保存", rgb(98, 107, 116))
			scheduleSave()
			return
		}
		stopScheduledSave()
		setStatus("内容与已保存版本一致", rgb(98, 107, 116))
	})
	dlg.Deactivating().Attach(func() {
		stopScheduledSave()
		if err := saveProgress(false); err != nil {
			setStatus("自动保存失败，请关闭前重试", rgb(184, 56, 43))
		}
	})
	dlg.Closing().Attach(func(canceled *bool, reason walk.CloseReason) {
		stopScheduledSave()
		if err := saveProgress(true); err != nil {
			*canceled = true
			setStatus("保存失败，请重试", rgb(184, 56, 43))
			walk.MsgBox(dlg, "保存进展失败", err.Error(), walk.MsgBoxOK|walk.MsgBoxIconError)
		}
	})

	progressInputEdit.SetFocus()
	dlg.Run()
	a.ensureCollapsedVisible()
}

func (a *NativeApp) openTagSearchDialog() {
	if a.mw == nil {
		return
	}
	a.beginDialog()
	defer a.endDialog()

	var dlg *walk.Dialog
	var headerHost *walk.Composite
	var cancelHost *walk.Composite
	var saveHost *walk.Composite
	var clearHost *walk.Composite
	var tagEdit *walk.LineEdit

	current := strings.Join(a.tagFilter, ", ")

	applyTags := func(tags []string) {
		a.tagFilter = tags
		a.applyTodos(a.store.List())
		dlg.Accept()
	}

	if err := (Dialog{
		AssignTo:  &dlg,
		Title:     "标签搜索",
		FixedSize: true,
		MinSize:   Size{Width: 460, Height: 280},
		MaxSize:   Size{Width: 460, Height: 280},
		Background: SolidColorBrush{
			Color: boardBackgroundColor(),
		},
		Layout: VBox{
			Margins: Margins{Left: 18, Top: 18, Right: 18, Bottom: 18},
			Spacing: 12,
		},
		Font: Font{Family: "Microsoft YaHei UI", PointSize: 10},
		OnKeyDown: func(key walk.Key) {
			if key == walk.KeyEscape {
				dlg.Cancel()
			}
		},
		Children: []Widget{
			Composite{
				AssignTo:   &headerHost,
				Background: SolidColorBrush{Color: boardBackgroundColor()},
				Layout: VBox{
					MarginsZero: true,
					SpacingZero: true,
				},
			},
			Composite{
				Background: SolidColorBrush{Color: rgb(252, 254, 255)},
				Layout: VBox{
					Margins: Margins{Left: 16, Top: 16, Right: 16, Bottom: 16},
					Spacing: 10,
				},
				Children: []Widget{
					Label{
						Text:      "输入标签（逗号分隔），仅显示匹配标签的事项。",
						TextColor: rgb(95, 103, 112),
					},
					Label{Text: "标签", TextColor: rgb(76, 88, 100)},
					LineEdit{
						AssignTo:   &tagEdit,
						Text:       current,
						Background: SolidColorBrush{Color: rgb(247, 250, 253)},
					},
				},
			},
			Composite{
				Background: SolidColorBrush{Color: rgb(252, 254, 255)},
				Layout: HBox{
					Margins: Margins{Left: 16, Top: 14, Right: 16, Bottom: 14},
					Spacing: 10,
				},
				Children: []Widget{
					HSpacer{},
					Composite{
						AssignTo:   &clearHost,
						Background: SolidColorBrush{Color: rgb(252, 254, 255)},
						Layout: VBox{
							MarginsZero: true,
							SpacingZero: true,
						},
					},
					Composite{
						AssignTo:   &cancelHost,
						Background: SolidColorBrush{Color: rgb(252, 254, 255)},
						Layout: VBox{
							MarginsZero: true,
							SpacingZero: true,
						},
					},
					Composite{
						AssignTo:   &saveHost,
						Background: SolidColorBrush{Color: rgb(252, 254, 255)},
						Layout: VBox{
							MarginsZero: true,
							SpacingZero: true,
						},
					},
				},
			},
		},
	}).Create(a.mw); err != nil {
		a.showError("打开标签搜索失败", err)
		return
	}

	prepareOverlayDialog(dlg)
	a.trackDialogWindow(dlg)
	if _, err := NewOverlayDialogHeaderWidget(headerHost, "标签搜索", "筛选主面板的待办事项。", func() {
		startWindowDrag(dlg.Handle())
	}); err != nil {
		dlg.Dispose()
		a.showError("创建标签搜索窗口头部失败", err)
		return
	}
	if _, err := NewOverlayDialogActionWidget(clearHost, "清除过滤", false, func() {
		applyTags(nil)
	}); err != nil {
		dlg.Dispose()
		a.showError("创建标签搜索按钮失败", err)
		return
	}
	if _, err := NewOverlayDialogActionWidget(cancelHost, "取消", false, func() {
		dlg.Cancel()
	}); err != nil {
		dlg.Dispose()
		a.showError("创建标签搜索按钮失败", err)
		return
	}
	if _, err := NewOverlayDialogActionWidget(saveHost, "应用", true, func() {
		applyTags(normalizeTags(tagEdit.Text()))
	}); err != nil {
		dlg.Dispose()
		a.showError("创建标签搜索按钮失败", err)
		return
	}

	tagEdit.SetFocus()
	dlg.Run()
	a.ensureCollapsedVisible()
}

func (a *NativeApp) openHistoryDialog() {
	if a.mw == nil {
		return
	}
	a.beginDialog()
	defer a.endDialog()

	var dlg *walk.Dialog
	var headerHost *walk.Composite
	var footerHost *walk.Composite
	var closeHost *walk.Composite
	var scrollView *walk.ScrollView
	var listWidget *TodoBoardWidget
	var err error

	history := a.store.HistoryList()
	work := desktopWorkArea()
	panelWidth := a.windowWidth
	if panelWidth <= 0 {
		panelWidth = targetExpandedWindowWidth(work.Width)
	}

	if err = (Dialog{
		AssignTo: &dlg,
		Title:    "\u5386\u53f2\u4e8b\u9879",
		MinSize:  Size{Width: panelWidth, Height: 640},
		MaxSize:  Size{Width: panelWidth, Height: 640},
		Background: SolidColorBrush{
			Color: boardBackgroundColor(),
		},
		Layout: VBox{
			Margins: Margins{Left: 18, Top: 18, Right: 18, Bottom: 18},
			Spacing: 12,
		},
		Font: Font{Family: "Microsoft YaHei UI", PointSize: 10},
		OnKeyDown: func(key walk.Key) {
			if key == walk.KeyEscape {
				dlg.Cancel()
			}
		},
		Children: []Widget{
			Composite{
				AssignTo:   &headerHost,
				Background: SolidColorBrush{Color: boardBackgroundColor()},
				Layout: VBox{
					MarginsZero: true,
					SpacingZero: true,
				},
			},
			ScrollView{
				AssignTo:        &scrollView,
				StretchFactor:   1,
				HorizontalFixed: true,
				Background:      SolidColorBrush{Color: boardBackgroundColor()},
				Layout: VBox{
					MarginsZero: true,
					SpacingZero: true,
					Alignment:   AlignHNearVNear,
				},
			},
			Composite{
				AssignTo:   &footerHost,
				Background: SolidColorBrush{Color: rgb(252, 254, 255)},
				Layout: HBox{
					Margins: Margins{Left: 16, Top: 14, Right: 16, Bottom: 14},
					Spacing: 10,
				},
				Children: []Widget{
					HSpacer{},
					Composite{
						AssignTo:   &closeHost,
						Background: SolidColorBrush{Color: rgb(252, 254, 255)},
						Layout: VBox{
							MarginsZero: true,
							SpacingZero: true,
						},
					},
				},
			},
		},
	}).Create(a.mw); err != nil {
		a.showError("\u6253\u5f00\u5386\u53f2\u7a97\u53e3\u5931\u8d25", err)
		return
	}

	prepareOverlayDialog(dlg)
	a.trackDialogWindow(dlg)
	_ = scrollView.SetDoubleBuffering(true)
	_ = dlg.SetBoundsPixels(walk.Rectangle{
		X:      work.X + (work.Width-panelWidth)/2,
		Y:      work.Y + (work.Height-640)/2,
		Width:  panelWidth,
		Height: 640,
	})
	if _, err = NewOverlayDialogHeaderWidget(headerHost, "\u5386\u53f2\u4e8b\u9879", "\u5df2\u5b8c\u6210\u4e8b\u9879\uff0c\u70b9\u51fb\u67e5\u770b\u8be6\u60c5\u5e76\u5220\u9664\u3002", func() {
		startWindowDrag(dlg.Handle())
	}); err != nil {
		dlg.Dispose()
		a.showError("\u521b\u5efa\u5386\u53f2\u7a97\u53e3\u5934\u90e8\u5931\u8d25", err)
		return
	}
	if _, err = NewOverlayDialogActionWidget(closeHost, "\u5173\u95ed", true, func() {
		dlg.Accept()
	}); err != nil {
		dlg.Dispose()
		a.showError("\u521b\u5efa\u5386\u53f2\u7a97\u53e3\u6309\u94ae\u5931\u8d25", err)
		return
	}

	painter := a.painter
	if painter == nil {
		painter, err = NewCardPainter(a.mw)
		if err != nil {
			dlg.Dispose()
			a.showError("\u521b\u5efa\u5386\u53f2\u7a97\u53e3\u753b\u7b14\u5931\u8d25", err)
			return
		}
		a.painter = painter
	}
	painter.SetUrgencyTintDays(a.settings.UrgencyTintDays)

	listWidget, err = NewTodoBoardWidget(scrollView, painter, nil, func(id string) {
		history, removeErr := a.store.RemoveHistory(id)
		if removeErr != nil {
			a.showError("\u5220\u9664\u5931\u8d25", removeErr)
			return
		}
		listWidget.SetTodos(history)
		a.refreshHistoryMenu()
	}, func(id string) {
		if a.openHistoryDetailDialog(id) {
			listWidget.SetTodos(a.store.HistoryList())
			a.refreshHistoryMenu()
		}
	}, "\u5220\u9664", false, true, false)
	if err != nil {
		dlg.Dispose()
		a.showError("\u521b\u5efa\u5386\u53f2\u5217\u8868\u5931\u8d25", err)
		return
	}
	listWidget.SetTodos(history)

	updateWidth := func() {
		if listWidget == nil || scrollView == nil {
			return
		}
		width := scrollView.ClientBoundsPixels().Width
		if width <= 0 {
			width = dlg.ClientBoundsPixels().Width - 36
		}
		listWidget.SetWidthHint(clampInt(width, minBoardWidth, 1200))
	}
	dlg.SizeChanged().Attach(func() {
		updateWidth()
	})
	scrollView.SizeChanged().Attach(func() {
		updateWidth()
	})
	updateWidth()

	dlg.Run()
	a.ensureCollapsedVisible()
}

func (a *NativeApp) openHistoryDetailDialog(id string) bool {
	if a.mw == nil {
		return false
	}
	a.beginDialog()
	defer a.endDialog()

	todo, ok := a.store.GetHistory(id)
	if !ok {
		walk.MsgBox(a.mw, "\u6253\u5f00\u5386\u53f2\u5931\u8d25", "\u6ca1\u6709\u627e\u5230\u5bf9\u5e94\u7684\u5386\u53f2\u4e8b\u9879\u3002", walk.MsgBoxOK|walk.MsgBoxIconWarning)
		return false
	}

	var dlg *walk.Dialog
	var headerHost *walk.Composite
	var closeHost *walk.Composite
	var deleteHost *walk.Composite
	deleted := false

	completedAt := parseStoredTime(todo.CompletedAt, parseStoredTime(todo.CreatedAt, time.Time{}))
	completedText := "\u672a\u8bb0\u5f55"
	if !completedAt.IsZero() {
		completedText = completedAt.Local().Format("2006-01-02 15:04")
	}

	deleteHistory := func() {
		result := walk.MsgBox(dlg, "\u5220\u9664\u5386\u53f2\u4e8b\u9879", "\u786e\u8ba4\u5220\u9664\u8be5\u5386\u53f2\u4e8b\u9879\uff1f\u6b64\u64cd\u4f5c\u65e0\u6cd5\u64a4\u9500\u3002", walk.MsgBoxYesNo|walk.MsgBoxIconWarning)
		if result != walk.DlgCmdYes {
			return
		}
		if _, err := a.store.RemoveHistory(id); err != nil {
			a.showError("\u5220\u9664\u5931\u8d25", err)
			return
		}
		deleted = true
		dlg.Accept()
	}

	if err := (Dialog{
		AssignTo: &dlg,
		Title:    "\u5386\u53f2\u4e8b\u9879",
		MinSize:  Size{Width: 620, Height: 640},
		Background: SolidColorBrush{
			Color: boardBackgroundColor(),
		},
		Layout: VBox{
			Margins: Margins{Left: 18, Top: 18, Right: 18, Bottom: 18},
			Spacing: 12,
		},
		Font: Font{Family: "Microsoft YaHei UI", PointSize: 10},
		OnKeyDown: func(key walk.Key) {
			if key == walk.KeyEscape {
				dlg.Cancel()
			}
		},
		Children: []Widget{
			Composite{
				AssignTo:   &headerHost,
				Background: SolidColorBrush{Color: boardBackgroundColor()},
				Layout: VBox{
					MarginsZero: true,
					SpacingZero: true,
				},
			},
			Composite{
				Background: SolidColorBrush{Color: rgb(252, 254, 255)},
				Layout: VBox{
					Margins: Margins{Left: 16, Top: 16, Right: 16, Bottom: 16},
					Spacing: 10,
				},
				Children: []Widget{
					Label{
						Text:      todo.Title,
						Font:      Font{Family: "Microsoft YaHei UI", PointSize: 14, Bold: true},
						TextColor: rgb(28, 34, 42),
					},
					Composite{
						Background: SolidColorBrush{Color: rgb(252, 254, 255)},
						Layout: Grid{
							Columns: 2,
							Margins: Margins{},
							Spacing: 8,
						},
						Children: []Widget{
							Label{Text: "\u5f85\u529e\u7c7b\u578b", TextColor: rgb(98, 107, 116)},
							Label{Text: "\u622a\u6b62\u65f6\u95f4", TextColor: rgb(98, 107, 116)},
							Label{Text: string(todo.Kind)},
							Label{Text: formatTodoDeadline(todo.Deadline)},
							Label{Text: "\u5b8c\u6210\u65f6\u95f4", TextColor: rgb(98, 107, 116)},
							Label{Text: completedText},
						},
					},
				},
			},
			Composite{
				Background: SolidColorBrush{Color: rgb(252, 254, 255)},
				Layout: VBox{
					Margins: Margins{Left: 16, Top: 16, Right: 16, Bottom: 16},
					Spacing: 8,
				},
				Children: []Widget{
					Label{
						Text:      "\u8be6\u60c5\u5185\u5bb9",
						TextColor: rgb(98, 107, 116),
					},
					TextEdit{
						Text:          detailText(todo.Details),
						Background:    SolidColorBrush{Color: rgb(247, 250, 253)},
						ReadOnly:      true,
						StretchFactor: 1,
						VScroll:       true,
						MinSize:       Size{Height: 140},
					},
				},
			},
			Composite{
				Background: SolidColorBrush{Color: rgb(252, 254, 255)},
				Layout: VBox{
					Margins: Margins{Left: 16, Top: 16, Right: 16, Bottom: 16},
					Spacing: 8,
				},
				Children: []Widget{
					Label{
						Text:      "\u6700\u540e\u8fdb\u5c55",
						TextColor: rgb(98, 107, 116),
					},
					TextEdit{
						Text:          progressTimeline(todo.Progress),
						Background:    SolidColorBrush{Color: rgb(247, 250, 253)},
						ReadOnly:      true,
						StretchFactor: 1,
						VScroll:       true,
						MinSize:       Size{Height: 180},
					},
				},
			},
			Composite{
				Background: SolidColorBrush{Color: rgb(252, 254, 255)},
				Layout: HBox{
					Margins: Margins{Left: 16, Top: 14, Right: 16, Bottom: 14},
					Spacing: 10,
				},
				Children: []Widget{
					HSpacer{},
					Composite{
						AssignTo:   &deleteHost,
						Background: SolidColorBrush{Color: rgb(252, 254, 255)},
						Layout: VBox{
							MarginsZero: true,
							SpacingZero: true,
						},
					},
					Composite{
						AssignTo:   &closeHost,
						Background: SolidColorBrush{Color: rgb(252, 254, 255)},
						Layout: VBox{
							MarginsZero: true,
							SpacingZero: true,
						},
					},
				},
			},
		},
	}).Create(a.mw); err != nil {
		a.showError("\u6253\u5f00\u5386\u53f2\u5931\u8d25", err)
		return false
	}

	prepareOverlayDialog(dlg)
	a.trackDialogWindow(dlg)
	if _, err := NewOverlayDialogHeaderWidget(headerHost, "\u5386\u53f2\u4e8b\u9879", "\u67e5\u770b\u5185\u5bb9\u5e76\u53ef\u5220\u9664\u3002", func() {
		startWindowDrag(dlg.Handle())
	}); err != nil {
		dlg.Dispose()
		a.showError("\u521b\u5efa\u5386\u53f2\u7a97\u53e3\u5934\u90e8\u5931\u8d25", err)
		return false
	}
	if _, err := NewOverlayDialogActionWidget(deleteHost, "\u5220\u9664", false, func() {
		deleteHistory()
	}); err != nil {
		dlg.Dispose()
		a.showError("\u521b\u5efa\u5220\u9664\u6309\u94ae\u5931\u8d25", err)
		return false
	}
	if _, err := NewOverlayDialogActionWidget(closeHost, "\u5173\u95ed", true, func() {
		dlg.Accept()
	}); err != nil {
		dlg.Dispose()
		a.showError("\u521b\u5efa\u5173\u95ed\u6309\u94ae\u5931\u8d25", err)
		return false
	}

	dlg.Run()
	a.ensureCollapsedVisible()
	return deleted
}

func (a *NativeApp) openSettingsDialog() {
	if a.mw == nil {
		return
	}
	a.beginDialog()
	defer a.endDialog()

	settings := normalizeAppSettings(a.settings)

	var dlg *walk.Dialog
	var headerHost *walk.Composite
	var cancelHost *walk.Composite
	var saveHost *walk.Composite
	var tintDaysEdit *walk.NumberEdit
	var reminderBox *walk.ComboBox
	var autoStartCheck *walk.CheckBox
	var backgroundPathEdit *walk.LineEdit
	var backgroundAlphaEdit *walk.NumberEdit
	var errLabel *walk.Label

	reminderLabels, reminderValues := reminderLeadOptions()

	saveSettings := func() {
		next := settings
		next.UrgencyTintDays = int(math.Round(tintDaysEdit.Value()))
		if reminderBox != nil {
			idx := reminderBox.CurrentIndex()
			if idx >= 0 && idx < len(reminderValues) {
				next.ReminderLeadMin = reminderValues[idx]
			}
		}
		if autoStartCheck != nil {
			next.AutoStart = autoStartCheck.Checked()
			next.AutoStartAsked = true
		}
		if backgroundPathEdit != nil {
			next.BackgroundPath = strings.TrimSpace(backgroundPathEdit.Text())
		}
		if backgroundAlphaEdit != nil {
			next.BackgroundAlpha = int(math.Round(backgroundAlphaEdit.Value()))
		}
		saved, err := a.store.UpdateSettings(next)
		if err != nil {
			errLabel.SetText(err.Error())
			return
		}

		errLabel.SetText("")
		a.ensureAutoStart(saved.AutoStart)
		a.applySettings(saved)
		dlg.Accept()
	}

	if err := (Dialog{
		AssignTo:  &dlg,
		Title:     "\u8bbe\u7f6e",
		FixedSize: true,
		MinSize:   Size{Width: 560, Height: 470},
		MaxSize:   Size{Width: 560, Height: 470},
		Background: SolidColorBrush{
			Color: boardBackgroundColor(),
		},
		Layout: VBox{
			Margins: Margins{Left: 18, Top: 18, Right: 18, Bottom: 18},
			Spacing: 12,
		},
		Font: Font{Family: "Microsoft YaHei UI", PointSize: 10},
		OnKeyDown: func(key walk.Key) {
			if key == walk.KeyEscape {
				dlg.Cancel()
			}
		},
		Children: []Widget{
			Composite{
				AssignTo:   &headerHost,
				Background: SolidColorBrush{Color: boardBackgroundColor()},
				Layout: VBox{
					MarginsZero: true,
					SpacingZero: true,
				},
			},
			Composite{
				Background: SolidColorBrush{Color: rgb(252, 254, 255)},
				Layout: VBox{
					Margins: Margins{Left: 16, Top: 16, Right: 16, Bottom: 16},
					Spacing: 10,
				},
				Children: []Widget{
					Label{
						Text:      "\u4e34\u671f\u4e0e\u80cc\u666f",
						Font:      Font{Family: "Microsoft YaHei UI", PointSize: 11, Bold: true},
						TextColor: rgb(36, 44, 53),
					},
					Composite{
						Background: SolidColorBrush{Color: rgb(252, 254, 255)},
						Layout: HBox{
							MarginsZero: true,
							Spacing:     10,
						},
						Children: []Widget{
							Label{
								Text:      "\u53d8\u8272\u9636\u6bb5",
								TextColor: rgb(76, 88, 100),
								MinSize:   Size{Width: 92},
							},
							NumberEdit{
								AssignTo:           &tintDaysEdit,
								Background:         SolidColorBrush{Color: rgb(247, 250, 253)},
								Decimals:           0,
								Increment:          1,
								MinValue:           1,
								MaxValue:           100,
								SpinButtonsVisible: true,
								MinSize:            Size{Width: 120},
								MaxSize:            Size{Width: 120},
							},
							Label{
								Text:      "%",
								TextColor: rgb(112, 121, 130),
							},
							HSpacer{},
						},
					},
					Label{
						Text:      "\u63d0\u524d\u63d0\u9192",
						TextColor: rgb(76, 88, 100),
					},
					ComboBox{
						AssignTo:   &reminderBox,
						Background: SolidColorBrush{Color: rgb(247, 250, 253)},
						Model:      reminderLabels,
					},
					CheckBox{
						AssignTo:   &autoStartCheck,
						Text:       "\u5f00\u673a\u542f\u52a8",
						Checked:    settings.AutoStart,
						Background: SolidColorBrush{Color: rgb(252, 254, 255)},
					},
					Label{
						Text:      "\u4e3b\u754c\u9762\u80cc\u666f PNG \u8def\u5f84",
						TextColor: rgb(76, 88, 100),
					},
					LineEdit{
						AssignTo:   &backgroundPathEdit,
						Text:       settings.BackgroundPath,
						Background: SolidColorBrush{Color: rgb(247, 250, 253)},
					},
					Composite{
						Background: SolidColorBrush{Color: rgb(252, 254, 255)},
						Layout: HBox{
							MarginsZero: true,
							Spacing:     10,
						},
						Children: []Widget{
							Label{
								Text:      "\u80cc\u666f\u900f\u660e\u5ea6",
								TextColor: rgb(76, 88, 100),
								MinSize:   Size{Width: 92},
							},
							NumberEdit{
								AssignTo:           &backgroundAlphaEdit,
								Background:         SolidColorBrush{Color: rgb(247, 250, 253)},
								Decimals:           0,
								Increment:          1,
								MinValue:           0,
								MaxValue:           100,
								SpinButtonsVisible: true,
								MinSize:            Size{Width: 120},
								MaxSize:            Size{Width: 120},
							},
							Label{
								Text:      "%",
								TextColor: rgb(112, 121, 130),
							},
							HSpacer{},
						},
					},
				},
			},
			Label{
				AssignTo:  &errLabel,
				TextColor: rgb(184, 56, 43),
			},
			Composite{
				Background: SolidColorBrush{Color: rgb(252, 254, 255)},
				Layout: HBox{
					Margins: Margins{Left: 16, Top: 14, Right: 16, Bottom: 14},
					Spacing: 10,
				},
				Children: []Widget{
					HSpacer{},
					Composite{
						AssignTo:   &cancelHost,
						Background: SolidColorBrush{Color: rgb(252, 254, 255)},
						Layout: VBox{
							MarginsZero: true,
							SpacingZero: true,
						},
					},
					Composite{
						AssignTo:   &saveHost,
						Background: SolidColorBrush{Color: rgb(252, 254, 255)},
						Layout: VBox{
							MarginsZero: true,
							SpacingZero: true,
						},
					},
				},
			},
		},
	}).Create(a.mw); err != nil {
		a.showError("\u6253\u5f00\u8bbe\u7f6e\u5931\u8d25", err)
		return
	}

	prepareOverlayDialog(dlg)
	a.trackDialogWindow(dlg)
	if _, err := NewOverlayDialogHeaderWidget(headerHost, "\u8bbe\u7f6e", "", func() {
		startWindowDrag(dlg.Handle())
	}); err != nil {
		dlg.Dispose()
		a.showError("\u521b\u5efa\u8bbe\u7f6e\u7a97\u53e3\u5934\u90e8\u5931\u8d25", err)
		return
	}
	if _, err := NewOverlayDialogActionWidget(cancelHost, "\u53d6\u6d88", false, func() {
		dlg.Cancel()
	}); err != nil {
		dlg.Dispose()
		a.showError("\u521b\u5efa\u8bbe\u7f6e\u6309\u94ae\u5931\u8d25", err)
		return
	}
	if _, err := NewOverlayDialogActionWidget(saveHost, "\u4fdd\u5b58\u8bbe\u7f6e", true, func() {
		saveSettings()
	}); err != nil {
		dlg.Dispose()
		a.showError("\u521b\u5efa\u8bbe\u7f6e\u6309\u94ae\u5931\u8d25", err)
		return
	}

	_ = tintDaysEdit.SetValue(float64(settings.UrgencyTintDays))
	if backgroundAlphaEdit != nil {
		_ = backgroundAlphaEdit.SetValue(float64(settings.BackgroundAlpha))
	}
	if reminderBox != nil {
		_ = reminderBox.SetCurrentIndex(reminderLeadIndex(settings.ReminderLeadMin))
	}
	dlg.Run()
	a.ensureCollapsedVisible()
}

func (a *NativeApp) showWindow() {
	a.setWindowCollapsed(false, true)
}

func (a *NativeApp) setSortMode(mode SortMode) {
	if mode == "" {
		mode = SortModePriority
	}
	if a.sortMode == mode && len(a.sourceTodos) == 0 {
		return
	}

	a.sortMode = mode
	if a.sortWidget != nil {
		a.sortWidget.SetSelected(mode)
	}
	if a.listWidget != nil {
		a.applyTodos(a.sourceTodos)
	}
}

func (a *NativeApp) applySettings(settings AppSettings) {
	a.settings = normalizeAppSettings(settings)
	if a.painter != nil {
		a.painter.SetUrgencyTintDays(a.settings.UrgencyTintDays)
	}
	a.reloadBackgroundSkin()
	a.reminderLead = time.Duration(a.settings.ReminderLeadMin) * time.Minute
	if a.listWidget != nil {
		a.listWidget.SetSkin(a.backgroundBitmap, a.settings.BackgroundAlpha)
		_ = a.listWidget.Invalidate()
	}
}

func (a *NativeApp) reloadBackgroundSkin() {
	if a.backgroundBitmap != nil {
		a.backgroundBitmap.Dispose()
		a.backgroundBitmap = nil
	}
	path := strings.Trim(strings.TrimSpace(os.ExpandEnv(a.settings.BackgroundPath)), "\"")
	if path == "" || a.mw == nil || a.mw.IsDisposed() {
		return
	}
	if !filepath.IsAbs(path) {
		if abs, err := filepath.Abs(path); err == nil {
			path = abs
		}
	}
	if _, err := os.Stat(path); err != nil {
		return
	}
	bitmap, err := walk.NewBitmapFromFileForDPI(path, a.mw.DPI())
	if err != nil {
		return
	}
	a.backgroundBitmap = bitmap
}

func (a *NativeApp) ensureCollapsedVisible() {
	if a.mw == nil || a.mw.IsDisposed() || !a.collapsed {
		return
	}
	win.ShowWindow(a.mw.Handle(), win.SW_SHOWNOACTIVATE)
	bounds := a.mw.BoundsPixels()
	_ = a.mw.SetBoundsPixels(bounds)
	win.SetWindowPos(
		a.mw.Handle(),
		win.HWND_TOPMOST,
		int32(bounds.X),
		int32(bounds.Y),
		int32(bounds.Width),
		int32(bounds.Height),
		win.SWP_NOACTIVATE,
	)
}

func (a *NativeApp) updateSearchLabel() {
	if a.searchWidget == nil {
		return
	}
	if len(a.tagFilter) == 0 {
		a.searchWidget.SetLabel("标签搜索")
		return
	}
	label := "标签: " + strings.Join(a.tagFilter, ", ")
	a.searchWidget.SetLabel(shortenText(label, 12, "标签搜索"))
}

func (a *NativeApp) applyRootLayoutState(collapsed bool) {
	if a.rootLayout == nil {
		return
	}
	if collapsed {
		_ = a.rootLayout.SetMargins(walk.Margins{HNear: 3, VNear: 3, HFar: 3, VFar: 3})
		_ = a.rootLayout.SetSpacing(0)
		return
	}
	_ = a.rootLayout.SetMargins(walk.Margins{HNear: 14, VNear: 16, HFar: 14, VFar: 16})
	_ = a.rootLayout.SetSpacing(12)
}

func (a *NativeApp) beginDialog() {
	a.dialogDepth++
}

func (a *NativeApp) endDialog() {
	if a.dialogDepth > 0 {
		a.dialogDepth--
	}
}

func (a *NativeApp) shouldCollapseOnDeactivate() bool {
	return false
}

func (a *NativeApp) beginInternalPointerAction() {
	a.internalPointerActive = true
}

func (a *NativeApp) endInternalPointerAction() {
	a.internalPointerActive = false
}

func (a *NativeApp) isPointerInteractingInsidePanel() bool {
	if a.mw == nil || a.mw.IsDisposed() {
		return false
	}

	var pt win.POINT
	if !win.GetCursorPos(&pt) {
		return false
	}

	hwnd := win.WindowFromPoint(pt)
	if hwnd == 0 {
		return false
	}

	main := a.mw.Handle()
	if hwnd == main || win.IsChild(main, hwnd) {
		return true
	}

	bounds := a.mw.BoundsPixels()
	return pt.X >= int32(bounds.X) &&
		pt.X <= int32(bounds.X+bounds.Width) &&
		pt.Y >= int32(bounds.Y) &&
		pt.Y <= int32(bounds.Y+bounds.Height)
}

func (a *NativeApp) installMainWndProc() {
	if a.mw == nil || a.mainWndProcOrig != 0 {
		return
	}
	hwnd := a.mw.Handle()
	mainWndProcMu.Lock()
	mainWndProcMap[hwnd] = a
	mainWndProcMu.Unlock()

	orig := win.SetWindowLongPtr(hwnd, win.GWLP_WNDPROC, mainWndProcPtr)
	if orig == 0 {
		mainWndProcMu.Lock()
		delete(mainWndProcMap, hwnd)
		mainWndProcMu.Unlock()
		return
	}
	a.mainWndProcOrig = orig
}

func (a *NativeApp) uninstallMainWndProc() {
	if a.mw == nil || a.mainWndProcOrig == 0 {
		return
	}
	hwnd := a.mw.Handle()
	_ = win.SetWindowLongPtr(hwnd, win.GWLP_WNDPROC, a.mainWndProcOrig)
	a.mainWndProcOrig = 0
	mainWndProcMu.Lock()
	delete(mainWndProcMap, hwnd)
	mainWndProcMu.Unlock()
}

func (a *NativeApp) registerSessionNotifications() {
	if a == nil || a.mw == nil || a.mw.IsDisposed() || a.sessionNotifyRegistered {
		return
	}
	ret, _, _ := procWTSRegisterSessionNotification.Call(uintptr(a.mw.Handle()), uintptr(notifThisSession))
	if ret != 0 {
		a.sessionNotifyRegistered = true
	}
}

func (a *NativeApp) unregisterSessionNotifications() {
	if a == nil || a.mw == nil || a.mw.IsDisposed() || !a.sessionNotifyRegistered {
		return
	}
	procWTSUnRegisterSessionNotification.Call(uintptr(a.mw.Handle()))
	a.sessionNotifyRegistered = false
}

func (a *NativeApp) registerTrackedWindow(hwnd win.HWND) {
	if a == nil || hwnd == 0 {
		return
	}
	a.trackedWindowsMu.Lock()
	if a.trackedWindows == nil {
		a.trackedWindows = map[win.HWND]struct{}{}
	}
	a.trackedWindows[hwnd] = struct{}{}
	a.trackedWindowsMu.Unlock()
}

func (a *NativeApp) unregisterTrackedWindow(hwnd win.HWND) {
	if a == nil || hwnd == 0 {
		return
	}
	a.trackedWindowsMu.Lock()
	delete(a.trackedWindows, hwnd)
	a.trackedWindowsMu.Unlock()
}

func (a *NativeApp) trackDialogWindow(dlg *walk.Dialog) {
	if a == nil || dlg == nil || dlg.IsDisposed() {
		return
	}
	hwnd := dlg.Handle()
	if hwnd == 0 {
		return
	}
	a.registerTrackedWindow(hwnd)
	dlg.Disposing().Attach(func() {
		a.unregisterTrackedWindow(hwnd)
	})
}

func (a *NativeApp) pointInsideAnyAppWindow(pt win.POINT) bool {
	a.trackedWindowsMu.RLock()
	defer a.trackedWindowsMu.RUnlock()

	for hwnd := range a.trackedWindows {
		if hwnd == 0 || !win.IsWindowVisible(hwnd) {
			continue
		}
		var rect win.RECT
		if !win.GetWindowRect(hwnd, &rect) {
			continue
		}
		if pt.X >= rect.Left && pt.X < rect.Right && pt.Y >= rect.Top && pt.Y < rect.Bottom {
			return true
		}
	}
	return false
}

func (a *NativeApp) installGlobalMouseHook() error {
	if a.mouseHook != 0 {
		return nil
	}

	a.processID = uint32(os.Getpid())
	module := win.GetModuleHandle(nil)
	hook, _, err := procSetWindowsHookExW.Call(
		uintptr(whMouseLL),
		mouseHookProcPtr,
		uintptr(module),
		0,
	)
	if hook == 0 {
		return err
	}

	mouseHookMu.Lock()
	mouseHookApp = a
	mouseHookMu.Unlock()
	a.mouseHook = hook
	return nil
}

func (a *NativeApp) uninstallGlobalMouseHook() {
	if a.mouseHook == 0 {
		mouseHookMu.Lock()
		if mouseHookApp == a {
			mouseHookApp = nil
		}
		mouseHookMu.Unlock()
		return
	}

	procUnhookWindowsHookEx.Call(a.mouseHook)
	a.mouseHook = 0

	mouseHookMu.Lock()
	if mouseHookApp == a {
		mouseHookApp = nil
	}
	mouseHookMu.Unlock()
}

func (a *NativeApp) handleGlobalMouseDown(pt win.POINT) {
	if a == nil || a.mw == nil || a.mw.IsDisposed() {
		return
	}
	if a.summaryDialog != nil && !a.summaryDialog.IsDisposed() {
		if a.pointInsideWindow(pt, a.summaryDialog.Handle()) {
			return
		}
		a.mw.Synchronize(func() {
			a.closeUnlockSummaryDialog()
		})
		return
	}
	if a.quitting || a.collapsed || !a.visible || a.dialogDepth > 0 {
		return
	}

	if a.pointInsideAnyAppWindow(pt) {
		return
	}

	a.mw.Synchronize(func() {
		if a.mw == nil || a.mw.IsDisposed() || a.quitting || a.collapsed || !a.visible || a.dialogDepth > 0 {
			return
		}
		a.collapseWindow()
	})
}

func (a *NativeApp) pointInsideWindow(pt win.POINT, hwnd win.HWND) bool {
	if hwnd == 0 || !win.IsWindowVisible(hwnd) {
		return false
	}
	var rect win.RECT
	if !win.GetWindowRect(hwnd, &rect) {
		return false
	}
	return pt.X >= rect.Left && pt.X < rect.Right && pt.Y >= rect.Top && pt.Y < rect.Bottom
}

func (a *NativeApp) primeWindowState() {
	if a.mw == nil || a.mw.IsDisposed() {
		return
	}

	work := desktopWorkArea()
	a.windowWidth = targetExpandedWindowWidth(work.Width)
	contentHeight := a.expandedChromeHeight() + a.listHeight()
	minClientHeight := a.minimumExpandedClientHeight(work.Height)
	maxClientHeight := int(float64(work.Height)*a.maxHeightRatio) - a.windowDecorationHeight()
	maxClientHeight = clampInt(maxClientHeight, minClientHeight, maxInt(a.maxWindowHeight, minClientHeight))
	clientHeight := clampInt(contentHeight, minClientHeight, maxClientHeight)

	a.lastExpandedBounds = walk.Rectangle{
		X:      work.X + work.Width - a.windowWidth - 24,
		Y:      clampInt(work.Y+42, work.Y+18, work.Y+work.Height-clientHeight-a.windowDecorationHeight()-18),
		Width:  a.windowWidth,
		Height: clientHeight + a.windowDecorationHeight(),
	}
}

func (a *NativeApp) rememberExpandedBounds() {
	if a.mw == nil || a.mw.IsDisposed() || a.collapsed {
		return
	}
	bounds := a.mw.BoundsPixels()
	if bounds.Width <= a.collapsedWidth || bounds.Height <= a.collapsedHeight {
		return
	}
	a.lastExpandedBounds = bounds
}

func (a *NativeApp) resolvedExpandedBounds(targetClient walk.Size, work walk.Rectangle) walk.Rectangle {
	decorationHeight := a.windowDecorationHeight()
	width := targetClient.Width
	height := targetClient.Height + decorationHeight

	bounds := a.lastExpandedBounds
	if bounds.Width <= 0 || bounds.Height <= 0 {
		bounds = walk.Rectangle{
			X:      work.X + work.Width - width - 24,
			Y:      work.Y + 42,
			Width:  width,
			Height: height,
		}
	}

	bounds.Width = width
	bounds.Height = height
	bounds.X = clampInt(bounds.X, work.X+18, work.X+work.Width-bounds.Width-18)
	bounds.Y = clampInt(bounds.Y, work.Y+18, work.Y+work.Height-bounds.Height-18)
	return bounds
}

func (a *NativeApp) rememberedExpandedClientHeight() int {
	if a.lastExpandedBounds.Height <= 0 {
		return 0
	}
	clientHeight := a.lastExpandedBounds.Height - a.windowDecorationHeight()
	if clientHeight < 0 {
		return 0
	}
	return clientHeight
}

func (a *NativeApp) hideWindow() {
	a.collapseWindow()
}

func (a *NativeApp) handleSessionUnlock() {
	today := time.Now().Format("2006-01-02")
	if a.lastUnlockSummaryDay == today {
		return
	}
	a.lastUnlockSummaryDay = today
	a.openUnlockSummaryDialog()
}

func (a *NativeApp) openUnlockSummaryDialog() {
	if a.mw == nil || a.mw.IsDisposed() {
		return
	}
	if a.summaryDialog != nil && !a.summaryDialog.IsDisposed() {
		a.summaryDialog.SetFocus()
		return
	}

	todos := sortVisibleTodos(a.store.List(), a.sortMode)
	summaryText := a.buildUnlockSummary(todos)

	var dlg *walk.Dialog
	var headerHost *walk.Composite
	var closeHost *walk.Composite

	if err := (Dialog{
		AssignTo:  &dlg,
		Title:     "今日待办简报",
		FixedSize: true,
		MinSize:   Size{Width: 680, Height: 620},
		MaxSize:   Size{Width: 680, Height: 620},
		Background: SolidColorBrush{
			Color: boardBackgroundColor(),
		},
		Layout: VBox{
			Margins: Margins{Left: 18, Top: 18, Right: 18, Bottom: 18},
			Spacing: 12,
		},
		Font: Font{Family: "Microsoft YaHei UI", PointSize: 10},
		OnKeyDown: func(key walk.Key) {
			if key == walk.KeyEscape {
				dlg.Cancel()
			}
		},
		Children: []Widget{
			Composite{
				AssignTo:   &headerHost,
				Background: SolidColorBrush{Color: boardBackgroundColor()},
				Layout: VBox{
					MarginsZero: true,
					SpacingZero: true,
				},
			},
			Composite{
				Background: SolidColorBrush{Color: rgb(252, 254, 255)},
				Layout: VBox{
					Margins: Margins{Left: 16, Top: 16, Right: 16, Bottom: 16},
					Spacing: 8,
				},
				Children: []Widget{
					Label{
						Text:      fmt.Sprintf("当前共 %d 项待办。点击窗口外部任意位置可关闭。", len(todos)),
						TextColor: rgb(98, 107, 116),
					},
					TextEdit{
						Text:          summaryText,
						Background:    SolidColorBrush{Color: rgb(247, 250, 253)},
						ReadOnly:      true,
						StretchFactor: 1,
						VScroll:       true,
					},
				},
			},
			Composite{
				Background: SolidColorBrush{Color: rgb(252, 254, 255)},
				Layout: HBox{
					Margins: Margins{Left: 16, Top: 14, Right: 16, Bottom: 14},
					Spacing: 10,
				},
				Children: []Widget{
					HSpacer{},
					Composite{
						AssignTo:   &closeHost,
						Background: SolidColorBrush{Color: rgb(252, 254, 255)},
						Layout: VBox{
							MarginsZero: true,
							SpacingZero: true,
						},
					},
				},
			},
		},
	}).Create(a.mw); err != nil {
		a.showError("打开解锁简报失败", err)
		return
	}

	a.summaryDialog = dlg
	prepareOverlayDialog(dlg)
	a.trackDialogWindow(dlg)
	if _, err := NewOverlayDialogHeaderWidget(headerHost, "今日待办简报", "每日首次解锁后展示当前待办及最近进展。", func() {
		startWindowDrag(dlg.Handle())
	}); err != nil {
		dlg.Dispose()
		a.summaryDialog = nil
		a.showError("创建解锁简报头部失败", err)
		return
	}
	if _, err := NewOverlayDialogActionWidget(closeHost, "关闭", true, func() {
		a.closeUnlockSummaryDialog()
	}); err != nil {
		dlg.Dispose()
		a.summaryDialog = nil
		a.showError("创建解锁简报按钮失败", err)
		return
	}

	dlg.Disposing().Attach(func() {
		if a.summaryDialog == dlg {
			a.summaryDialog = nil
		}
	})
	dlg.Show()
	centerDialogOnWorkArea(dlg)
	activateHandle(dlg.Handle())
	dlg.SetFocus()
}

func (a *NativeApp) closeUnlockSummaryDialog() {
	if a.summaryDialog == nil || a.summaryDialog.IsDisposed() {
		return
	}
	a.summaryDialog.Close(0)
}

func (a *NativeApp) buildUnlockSummary(todos []Todo) string {
	if len(todos) == 0 {
		return "当前没有待办。\r\n\r\n今天可以轻装上阵。"
	}

	lines := make([]string, 0, len(todos)*7)
	for i, todo := range todos {
		lines = append(lines, fmt.Sprintf("%d. %s", i+1, todo.Title))
		lines = append(lines, "   类型: "+string(todo.Kind))
		lines = append(lines, "   截止: "+formatTodoDeadline(todo.Deadline))
		if details := strings.TrimSpace(todo.Details); details != "" {
			lines = append(lines, "   详情: "+details)
		}
		progress := strings.TrimSpace(progressTimeline(todo.Progress))
		if progress == "" {
			lines = append(lines, "   进展: 暂无进展记录")
		} else {
			parts := strings.Split(progress, "\n")
			if len(parts) > 3 {
				parts = parts[len(parts)-3:]
			}
			lines = append(lines, "   最近进展:")
			for _, part := range parts {
				lines = append(lines, "   - "+strings.TrimSpace(part))
			}
		}
		if tag := firstTag(todo.Tags); tag != "" {
			lines = append(lines, "   标签: "+tag)
		}
		lines = append(lines, "")
	}
	return strings.Join(lines, "\r\n")
}

func (a *NativeApp) collapseWindow() {
	a.rememberExpandedBounds()
	a.setWindowCollapsed(true, false)
}

func (a *NativeApp) setWindowCollapsed(collapsed, focus bool) {
	if a.mw == nil {
		return
	}
	if a.collapsed == collapsed && a.mw.Visible() {
		if focus {
			activateHandle(a.mw.Handle())
			a.mw.SetFocus()
		}
		return
	}

	a.collapsed = collapsed
	a.visible = true

	a.mw.SetSuspended(true)
	a.applyRootLayoutState(collapsed)
	if a.headerHost != nil {
		a.headerHost.SetVisible(!collapsed)
	}
	if a.listHost != nil {
		a.listHost.SetVisible(!collapsed)
	}
	if a.footerHost != nil {
		a.footerHost.SetVisible(!collapsed)
	}
	if a.dockHost != nil {
		a.dockHost.SetVisible(collapsed)
	}
	a.mw.SetSuspended(false)

	applyMainWindowChrome(a.mw.Handle(), a.collapsed)
	a.syncWindowSize()
	a.refreshNotifyActions()
	if a.collapsed {
		win.ShowWindow(a.mw.Handle(), win.SW_SHOWNOACTIVATE)
	} else {
		a.mw.Show()
	}
	if focus {
		activateHandle(a.mw.Handle())
		a.mw.SetFocus()
	}
}

func (a *NativeApp) syncWindowSize() {
	if a.mw == nil || a.syncingWindowSize {
		return
	}

	a.syncingWindowSize = true
	defer func() {
		a.syncingWindowSize = false
	}()

	work := desktopWorkArea()
	a.windowWidth = targetExpandedWindowWidth(work.Width)
	minExpandedHeight := a.minimumExpandedClientHeight(work.Height)
	if a.collapsed {
		_ = a.mw.SetMinMaxSize(
			walk.Size{Width: a.collapsedWidth, Height: a.collapsedHeight},
			walk.Size{Width: a.collapsedWidth, Height: a.collapsedHeight},
		)
		targetClient := walk.Size{Width: a.collapsedWidth, Height: a.collapsedHeight}
		if a.mw.ClientBoundsPixels().Size() != targetClient {
			_ = a.mw.SetClientSizePixels(targetClient)
		}

		outer := a.mw.BoundsPixels()
		outer.X = work.X + work.Width - outer.Width
		outer.Y = clampInt(work.Y+(work.Height-outer.Height)/2, work.Y+24, work.Y+work.Height-outer.Height-24)
		_ = a.mw.SetBoundsPixels(outer)
		a.applyWindowShape(true)
		return
	}

	_ = a.mw.SetMinMaxSize(
		walk.Size{Width: a.windowWidth, Height: minExpandedHeight},
		walk.Size{Width: a.windowWidth, Height: a.maxWindowHeight},
	)

	if a.listWidget != nil {
		a.listWidget.SetWidthHint(a.boardWidthHint())
	}

	contentHeight := a.expandedChromeHeight() + a.listHeight()
	maxClientHeight := int(float64(work.Height)*a.maxHeightRatio) - a.windowDecorationHeight()
	maxClientHeight = clampInt(maxClientHeight, minExpandedHeight, maxInt(a.maxWindowHeight, minExpandedHeight))

	clientHeight := clampInt(contentHeight, minExpandedHeight, maxClientHeight)
	if rememberedHeight := a.rememberedExpandedClientHeight(); rememberedHeight > 0 {
		clientHeight = clampInt(rememberedHeight, minExpandedHeight, maxClientHeight)
	}
	targetClient := walk.Size{Width: a.windowWidth, Height: clientHeight}
	if a.mw.ClientBoundsPixels().Size() != targetClient {
		_ = a.mw.SetClientSizePixels(targetClient)
	}

	if a.listWidget != nil {
		a.listWidget.SetWidthHint(a.boardWidthHint())
	}

	outer := a.resolvedExpandedBounds(targetClient, work)
	_ = a.mw.SetBoundsPixels(outer)
	a.applyWindowShape(false)
}

func (a *NativeApp) expandedChromeHeight() int {
	headerHeight := 150
	if a.headerWidget != nil {
		headerHeight = a.headerWidget.CurrentHeight()
	}
	if a.sortWidget != nil {
		headerHeight += 10 + a.sortWidget.CurrentHeight()
	}

	const (
		windowMargins = 32
		stackSpacing  = 24
	)
	footerHeight := 42
	if a.actionWidget != nil {
		footerHeight = a.actionWidget.CurrentHeight()
	}
	if a.searchWidget != nil && a.searchWidget.CurrentHeight() > footerHeight {
		footerHeight = a.searchWidget.CurrentHeight()
	}

	return headerHeight + footerHeight + windowMargins + stackSpacing
}

func (a *NativeApp) minimumExpandedClientHeight(workHeight int) int {
	minimumListViewportHeight := int(math.Round(float64(workHeight) * 0.60))
	if minimumListViewportHeight < 170 {
		minimumListViewportHeight = 170
	}
	minHeight := a.expandedChromeHeight() + minimumListViewportHeight
	if a.minWindowHeight > 0 && a.minWindowHeight > minHeight {
		return a.minWindowHeight
	}
	return minHeight
}

func (a *NativeApp) listHeight() int {
	if a.listWidget == nil {
		return 120
	}

	return a.listWidget.PreferredHeight(a.boardWidthHint())
}

func (a *NativeApp) boardWidthHint() int {
	width := a.windowWidth - 28
	if a.listHost != nil {
		if current := a.listHost.ClientBoundsPixels().Width; current > 18 {
			width = current
		}
	}
	return clampInt(width, minBoardWidth, a.windowWidth)
}

func (a *NativeApp) windowDecorationHeight() int {
	if a.mw == nil {
		return 24
	}

	return a.mw.BoundsPixels().Height - a.mw.ClientBoundsPixels().Height
}

func (a *NativeApp) setupNotifyIcon() error {
	if a.mw == nil || a.notifyIcon != nil {
		return nil
	}

	icon, err := walk.NewIconFromResourceId(7)
	if err != nil {
		icon = walk.IconInformation()
	}
	a.appIcon = icon
	if a.appIcon != nil {
		_ = a.mw.SetIcon(a.appIcon)
	}

	notifyIcon, err := walk.NewNotifyIcon(a.mw)
	if err != nil {
		return err
	}
	a.notifyIcon = notifyIcon

	if a.appIcon != nil {
		_ = a.notifyIcon.SetIcon(a.appIcon)
	}
	_ = a.notifyIcon.SetToolTip("todoooo")
	_ = a.notifyIcon.SetVisible(true)

	a.notifyIcon.MouseUp().Attach(func(x, y int, button walk.MouseButton) {
		if button != walk.LeftButton || a.mw == nil || a.mw.IsDisposed() {
			return
		}
		a.mw.Synchronize(func() {
			if !a.visible || a.collapsed {
				a.showWindow()
				return
			}
			a.collapseWindow()
		})
	})
	a.notifyIcon.MessageClicked().Attach(func() {
		if a.mw == nil || a.mw.IsDisposed() {
			return
		}
		a.mw.Synchronize(func() {
			a.showWindow()
		})
	})

	expandAction := walk.NewAction()
	_ = expandAction.SetText("\u5c55\u5f00\u4e3b\u754c\u9762")
	expandAction.Triggered().Attach(func() {
		a.showWindow()
	})
	a.trayExpandAction = expandAction

	collapseAction := walk.NewAction()
	_ = collapseAction.SetText("\u6536\u8d77\u5230\u53f3\u4fa7\u6807\u7b7e")
	collapseAction.Triggered().Attach(func() {
		a.collapseWindow()
	})
	a.trayCollapseAction = collapseAction

	historyMenu, _ := walk.NewMenu()
	historyAction := walk.NewMenuAction(historyMenu)
	_ = historyAction.SetText("\u5386\u53f2\u4e8b\u9879")
	a.trayHistoryAction = historyAction
	a.trayHistoryMenu = historyMenu

	settingsAction := walk.NewAction()
	_ = settingsAction.SetText("\u8bbe\u7f6e")
	settingsAction.Triggered().Attach(func() {
		a.openSettingsDialog()
	})
	a.traySettingsAction = settingsAction

	exitAction := walk.NewAction()
	_ = exitAction.SetText("\u9000\u51fa")
	exitAction.Triggered().Attach(func() {
		a.exitApp()
	})

	actions := a.notifyIcon.ContextMenu().Actions()
	_ = actions.Add(expandAction)
	_ = actions.Add(collapseAction)
	_ = actions.Add(historyAction)
	_ = actions.Add(settingsAction)
	_ = actions.Add(walk.NewSeparatorAction())
	_ = actions.Add(exitAction)
	a.refreshHistoryMenu()
	a.refreshNotifyActions()
	return nil
}

func (a *NativeApp) refreshNotifyActions() {
	if a.trayExpandAction != nil {
		_ = a.trayExpandAction.SetVisible(a.collapsed || !a.visible)
	}
	if a.trayCollapseAction != nil {
		_ = a.trayCollapseAction.SetVisible(a.visible && !a.collapsed)
	}
}

func (a *NativeApp) refreshHistoryMenu() {
	if a.trayHistoryMenu == nil || a.trayHistoryAction == nil {
		return
	}

	actions := a.trayHistoryMenu.Actions()
	actions.Clear()

	openPanel := walk.NewAction()
	_ = openPanel.SetText("\u6253\u5f00\u5386\u53f2\u9762\u677f")
	openPanel.Triggered().Attach(func() {
		a.openHistoryDialog()
	})
	_ = actions.Add(openPanel)
	_ = actions.Add(walk.NewSeparatorAction())

	history := a.store.HistoryList()
	if len(history) == 0 {
		empty := walk.NewAction()
		_ = empty.SetText("\u6682\u65e0\u5386\u53f2\u4e8b\u9879")
		_ = empty.SetEnabled(false)
		_ = actions.Add(empty)
		return
	}

	sort.SliceStable(history, func(i, j int) bool {
		left := parseStoredTime(history[i].CompletedAt, parseStoredTime(history[i].CreatedAt, time.Time{}))
		right := parseStoredTime(history[j].CompletedAt, parseStoredTime(history[j].CreatedAt, time.Time{}))
		if left.Equal(right) {
			return history[i].Title < history[j].Title
		}
		return left.After(right)
	})

	maxItems := 12
	if len(history) < maxItems {
		maxItems = len(history)
	}
	for i := 0; i < maxItems; i++ {
		todo := history[i]
		action := walk.NewAction()
		_ = action.SetText(historyMenuLabel(todo))
		action.Triggered().Attach(func() {
			if a.openHistoryDetailDialog(todo.ID) {
				a.refreshHistoryMenu()
			}
		})
		_ = actions.Add(action)
	}
}

func (a *NativeApp) startDeadlineWatcher() {
	if a.deadlinePoll <= 0 || a.notificationStop != nil {
		return
	}

	a.notificationStop = make(chan struct{})
	go func() {
		ticker := time.NewTicker(a.deadlinePoll)
		defer ticker.Stop()

		a.checkDeadlineNotifications()
		for {
			select {
			case <-ticker.C:
				a.checkDeadlineNotifications()
			case <-a.notificationStop:
				return
			}
		}
	}()
}

func (a *NativeApp) checkDeadlineNotifications() {
	now := time.Now()
	todos := a.store.List()

	for _, todo := range todos {
		deadline := parseStoredTime(todo.Deadline, time.Time{})
		if deadline.IsZero() {
			continue
		}

		if a.reminderLead > 0 && now.Before(deadline) {
			remindAt := deadline.Add(-a.reminderLead)
			if !a.reminded[todo.ID] && (now.Equal(remindAt) || now.After(remindAt)) {
				a.reminded[todo.ID] = true
				if a.mw == nil || a.mw.IsDisposed() {
					return
				}
				todo := todo
				a.mw.Synchronize(func() {
					a.showReminderNotification(todo, deadline)
				})
			}
		}

		if a.notified[todo.ID] || deadline.After(now) {
			continue
		}

		a.notified[todo.ID] = true
		if a.mw == nil || a.mw.IsDisposed() {
			return
		}
		todo := todo
		a.mw.Synchronize(func() {
			a.showDeadlineNotification(todo)
		})
	}
}

func (a *NativeApp) showDeadlineNotification(todo Todo) {
	if a.notifyIcon != nil {
		_ = a.notifyIcon.ShowInfo(
			"\u5f85\u529e\u5230\u671f\u63d0\u9192",
			"\u300a"+todo.Title+"\u300b\u5df2\u5230\u622a\u6b62\u65f6\u95f4",
		)
		return
	}

	if a.mw != nil && !a.mw.IsDisposed() {
		walk.MsgBox(
			a.mw,
			"\u5f85\u529e\u5230\u671f\u63d0\u9192",
			"\u300a"+todo.Title+"\u300b\u5df2\u5230\u622a\u6b62\u65f6\u95f4",
			walk.MsgBoxOK|walk.MsgBoxIconInformation,
		)
	}
}

func (a *NativeApp) syncNotificationMaps(todos []Todo) {
	if a.notified == nil {
		a.notified = map[string]bool{}
	}
	if a.reminded == nil {
		a.reminded = map[string]bool{}
	}

	live := make(map[string]struct{}, len(todos))
	for _, todo := range todos {
		live[todo.ID] = struct{}{}
	}

	for id := range a.notified {
		if _, ok := live[id]; !ok {
			delete(a.notified, id)
		}
	}
	for id := range a.reminded {
		if _, ok := live[id]; !ok {
			delete(a.reminded, id)
		}
	}
}

func (a *NativeApp) showReminderNotification(todo Todo, deadline time.Time) {
	timeText := deadline.Local().Format("15:04")
	if a.notifyIcon != nil {
		_ = a.notifyIcon.ShowInfo(
			"\u5f85\u529e\u63d0\u524d\u63d0\u9192",
			"\u300a"+todo.Title+"\u300b\u5c06\u5728 "+timeText+"\u5230\u671f",
		)
		return
	}

	if a.mw != nil && !a.mw.IsDisposed() {
		walk.MsgBox(
			a.mw,
			"\u5f85\u529e\u63d0\u524d\u63d0\u9192",
			"\u300a"+todo.Title+"\u300b\u5c06\u5728 "+timeText+"\u5230\u671f",
			walk.MsgBoxOK|walk.MsgBoxIconInformation,
		)
	}
}

func (a *NativeApp) exitApp() {
	if a.quitting || a.mw == nil {
		return
	}

	a.quitting = true
	a.uninstallGlobalMouseHook()
	a.uninstallMainWndProc()
	a.unregisterSessionNotifications()
	a.unregisterTrackedWindow(a.mw.Handle())
	if a.notificationStop != nil {
		close(a.notificationStop)
		a.notificationStop = nil
	}
	if a.notifyIcon != nil {
		_ = a.notifyIcon.Dispose()
		a.notifyIcon = nil
	}
	if a.backgroundBitmap != nil {
		a.backgroundBitmap.Dispose()
		a.backgroundBitmap = nil
	}
	a.mw.Close()
}

func (a *NativeApp) showError(title string, err error) {
	if err == nil || a.mw == nil {
		return
	}

	walk.MsgBox(a.mw, title, err.Error(), walk.MsgBoxOK|walk.MsgBoxIconError)
}

func desktopWorkArea() walk.Rectangle {
	var rc win.RECT
	const spiGetWorkArea = 0x0030

	if win.SystemParametersInfo(spiGetWorkArea, 0, unsafe.Pointer(&rc), 0) {
		return walk.Rectangle{
			X:      int(rc.Left),
			Y:      int(rc.Top),
			Width:  int(rc.Right - rc.Left),
			Height: int(rc.Bottom - rc.Top),
		}
	}

	return walk.Rectangle{
		X:      0,
		Y:      0,
		Width:  int(win.GetSystemMetrics(win.SM_CXSCREEN)),
		Height: int(win.GetSystemMetrics(win.SM_CYSCREEN)),
	}
}

func targetExpandedWindowWidth(workWidth int) int {
	return expandedWindowWidth
}

func activateHandle(hwnd win.HWND) {
	if hwnd == 0 {
		return
	}

	const flags = win.SWP_NOMOVE | win.SWP_NOSIZE | win.SWP_NOACTIVATE

	win.ShowWindow(hwnd, win.SW_RESTORE)
	win.SetWindowPos(hwnd, win.HWND_TOPMOST, 0, 0, 0, 0, flags)
	win.BringWindowToTop(hwnd)
	win.SetForegroundWindow(hwnd)
	win.SetActiveWindow(hwnd)
}

func startWindowDrag(hwnd win.HWND) {
	if hwnd == 0 {
		return
	}

	win.ReleaseCapture()
	win.SendMessage(hwnd, win.WM_NCLBUTTONDOWN, uintptr(win.HTCAPTION), 0)
}

func applyMainWindowChrome(hwnd win.HWND, collapsed bool) {
	if hwnd == 0 {
		return
	}

	style := uint32(win.GetWindowLong(hwnd, win.GWL_STYLE))
	if collapsed {
		style &^= win.WS_CAPTION | win.WS_THICKFRAME | win.WS_MINIMIZEBOX | win.WS_MAXIMIZEBOX | win.WS_SYSMENU
		style |= win.WS_POPUP
	} else {
		style &^= win.WS_POPUP | win.WS_MINIMIZEBOX | win.WS_MAXIMIZEBOX
		style |= win.WS_CAPTION | win.WS_SYSMENU | win.WS_THICKFRAME
	}
	win.SetWindowLong(hwnd, win.GWL_STYLE, int32(style))

	exStyle := uint32(win.GetWindowLong(hwnd, win.GWL_EXSTYLE))
	exStyle &^= win.WS_EX_APPWINDOW
	exStyle |= win.WS_EX_TOOLWINDOW | win.WS_EX_LAYERED
	win.SetWindowLong(hwnd, win.GWL_EXSTYLE, int32(exStyle))

	if procSetLayeredWindowAttributes.Find() == nil {
		procSetLayeredWindowAttributes.Call(uintptr(hwnd), 0, uintptr(overlayOpacity), layeredAlphaFlag)
	}

	win.SetWindowPos(
		hwnd,
		win.HWND_TOPMOST,
		0,
		0,
		0,
		0,
		win.SWP_NOMOVE|win.SWP_NOSIZE|win.SWP_FRAMECHANGED|win.SWP_NOACTIVATE,
	)
}

func applyOverlayPopupChrome(hwnd win.HWND) {
	if hwnd == 0 {
		return
	}

	style := uint32(win.GetWindowLong(hwnd, win.GWL_STYLE))
	style &^= win.WS_POPUP | win.WS_MINIMIZEBOX | win.WS_MAXIMIZEBOX
	style |= win.WS_CAPTION | win.WS_SYSMENU | win.WS_THICKFRAME
	win.SetWindowLong(hwnd, win.GWL_STYLE, int32(style))

	exStyle := uint32(win.GetWindowLong(hwnd, win.GWL_EXSTYLE))
	exStyle &^= win.WS_EX_APPWINDOW | win.WS_EX_TOOLWINDOW
	exStyle |= win.WS_EX_LAYERED
	win.SetWindowLong(hwnd, win.GWL_EXSTYLE, int32(exStyle))

	if procSetLayeredWindowAttributes.Find() == nil {
		procSetLayeredWindowAttributes.Call(uintptr(hwnd), 0, uintptr(overlayOpacity), layeredAlphaFlag)
	}

	win.SetWindowPos(
		hwnd,
		win.HWND_TOPMOST,
		0,
		0,
		0,
		0,
		win.SWP_NOMOVE|win.SWP_NOSIZE|win.SWP_FRAMECHANGED|win.SWP_NOACTIVATE,
	)
}

func prepareOverlayDialog(dlg *walk.Dialog) {
	if dlg == nil || dlg.IsDisposed() {
		return
	}

	applyOverlayPopupChrome(dlg.Handle())
	centerDialogOnWorkArea(dlg)
}

func centerDialogOnWorkArea(dlg *walk.Dialog) {
	if dlg == nil || dlg.IsDisposed() {
		return
	}

	work := desktopWorkArea()
	bounds := dlg.BoundsPixels()
	if bounds.Width <= 0 || bounds.Height <= 0 {
		return
	}

	bounds.X = work.X + (work.Width-bounds.Width)/2
	bounds.Y = work.Y + (work.Height-bounds.Height)/2
	_ = dlg.SetBoundsPixels(bounds)
}

func (a *NativeApp) ensureAutoStart(enabled bool) {
	exe, err := os.Executable()
	if err != nil {
		return
	}
	exe = filepath.Clean(exe)

	startupDir := os.Getenv("APPDATA")
	if startupDir == "" {
		return
	}
	startupDir = filepath.Join(startupDir, "Microsoft", "Windows", "Start Menu", "Programs", "Startup")
	if err := os.MkdirAll(startupDir, 0o755); err != nil {
		return
	}

	scriptPath := filepath.Join(startupDir, "TodoLauncher.cmd")
	if enabled {
		content := "@echo off\r\nstart \"\" \"" + exe + "\"\r\n"
		_ = os.WriteFile(scriptPath, []byte(content), 0o644)
		return
	}
	_ = os.Remove(scriptPath)
}

func (a *NativeApp) applyWindowShape(collapsed bool) {
	if a.mw == nil || a.mw.IsDisposed() || procSetWindowRgn.Find() != nil {
		return
	}
	procSetWindowRgn.Call(uintptr(a.mw.Handle()), 0, 1)
}

func deadlineSelectorOptions() ([]string, []string) {
	deadlineHours := make([]string, 24)
	for hour := range deadlineHours {
		deadlineHours[hour] = fmt.Sprintf("%02d", hour)
	}

	deadlineMinutes := make([]string, 60)
	for minute := range deadlineMinutes {
		deadlineMinutes[minute] = fmt.Sprintf("%02d", minute)
	}

	return deadlineHours, deadlineMinutes
}

func composeDeadlineValue(date time.Time, hourText, minuteText string) (string, error) {
	if date.IsZero() {
		return "", fmt.Errorf("请选择截止日期")
	}

	hour, err := strconv.Atoi(hourText)
	if err != nil {
		return "", fmt.Errorf("请选择截止小时")
	}
	minute, err := strconv.Atoi(minuteText)
	if err != nil {
		return "", fmt.Errorf("请选择截止分钟")
	}

	deadline := time.Date(date.Year(), date.Month(), date.Day(), hour, minute, 0, 0, time.Local)
	return deadline.Format("2006-01-02 15:04"), nil
}

func mainWndProc(hwnd win.HWND, msg uint32, wParam, lParam uintptr) uintptr {
	mainWndProcMu.Lock()
	app := mainWndProcMap[hwnd]
	mainWndProcMu.Unlock()

	if app != nil {
		switch msg {
		case wmWTSSessionChange:
			if uint32(wParam) == wtsSessionUnlock && app.mw != nil && !app.mw.IsDisposed() {
				app.mw.Synchronize(func() {
					app.handleSessionUnlock()
				})
			}
		case win.WM_LBUTTONDOWN, win.WM_RBUTTONDOWN, win.WM_MBUTTONDOWN, win.WM_NCLBUTTONDOWN:
			app.beginInternalPointerAction()
		case win.WM_LBUTTONUP, win.WM_RBUTTONUP, win.WM_MBUTTONUP, win.WM_NCLBUTTONUP, win.WM_CAPTURECHANGED:
			app.endInternalPointerAction()
		case win.WM_ENTERSIZEMOVE:
			app.beginInternalPointerAction()
		case win.WM_EXITSIZEMOVE:
			app.endInternalPointerAction()
			app.rememberExpandedBounds()
		}
		if app.mainWndProcOrig != 0 {
			return win.CallWindowProc(app.mainWndProcOrig, hwnd, msg, wParam, lParam)
		}
	}
	return win.DefWindowProc(hwnd, msg, wParam, lParam)
}

func globalMouseHookProc(code int32, wParam, lParam uintptr) uintptr {
	if code == hcAction {
		switch uint32(wParam) {
		case win.WM_LBUTTONDOWN, win.WM_RBUTTONDOWN, win.WM_MBUTTONDOWN, win.WM_XBUTTONDOWN:
			hook := (*msllhookstruct)(unsafe.Pointer(lParam))
			if hook != nil {
				mouseHookMu.Lock()
				app := mouseHookApp
				mouseHookMu.Unlock()
				if app != nil {
					app.handleGlobalMouseDown(hook.Pt)
				}
			}
		}
	}

	result, _, _ := procCallNextHookEx.Call(0, uintptr(code), wParam, lParam)
	return result
}

func filterTodosByTags(todos []Todo, tags []string) []Todo {
	if len(tags) == 0 {
		return todos
	}
	out := make([]Todo, 0, len(todos))
	for _, todo := range todos {
		if hasAnyTag(todo.Tags, tags) {
			out = append(out, todo)
		}
	}
	return out
}

func todoKindCounts(todos []Todo) map[TodoType]int {
	counts := map[TodoType]int{
		TodoUrgentImportant:    0,
		TodoUrgentNotImportant: 0,
		TodoImportantNotUrgent: 0,
		TodoNeitherImportant:   0,
	}
	for _, todo := range todos {
		counts[todo.Kind]++
	}
	return counts
}

func hasAnyTag(todoTags []string, filter []string) bool {
	if len(filter) == 0 {
		return true
	}
	for _, tag := range todoTags {
		for _, want := range filter {
			if strings.EqualFold(tag, want) {
				return true
			}
		}
	}
	return false
}

func tagsKey(tags []string) string {
	if len(tags) == 0 {
		return ""
	}
	normalized := make([]string, 0, len(tags))
	for _, tag := range tags {
		t := strings.TrimSpace(tag)
		if t == "" {
			continue
		}
		normalized = append(normalized, strings.ToLower(t))
	}
	sort.Strings(normalized)
	return strings.Join(normalized, "|")
}

func reminderLeadOptions() ([]string, []int) {
	return []string{
			"\u4e0d\u63d0\u524d\u63d0\u9192",
			"5 \u5206\u949f",
			"10 \u5206\u949f",
			"30 \u5206\u949f",
			"1 \u5c0f\u65f6",
			"2 \u5c0f\u65f6",
			"6 \u5c0f\u65f6",
			"1 \u5929",
		},
		[]int{0, 5, 10, 30, 60, 120, 360, 1440}
}

func reminderLeadIndex(value int) int {
	_, values := reminderLeadOptions()
	for i, v := range values {
		if v == value {
			return i
		}
	}
	return 0
}

func normalizeProgressValue(value string) string {
	return strings.ReplaceAll(value, "\r\n", "\n")
}

func rgb(r, g, b byte) walk.Color {
	return walk.RGB(r, g, b)
}

func clampInt(value, min, max int) int {
	if max < min {
		max = min
	}
	if value < min {
		return min
	}
	if value > max {
		return max
	}
	return value
}

func detailText(value string) string {
	if value == "" {
		return "\u6682\u65e0\u8be6\u60c5\u5185\u5bb9\u3002"
	}
	return value
}

func progressTimeline(value string) string {
	value = strings.TrimSpace(strings.ReplaceAll(value, "\r\n", "\n"))
	if value == "" {
		return "\u6682\u65e0\u8fdb\u5c55\u8bb0\u5f55\u3002"
	}

	lines := strings.Split(value, "\n")
	formatted := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if len(line) > 11 && line[4] == '-' && line[7] == '-' && line[10] == ' ' {
			formatted = append(formatted, line[:10]+"    "+strings.TrimSpace(line[11:]))
			continue
		}
		formatted = append(formatted, line)
	}
	if len(formatted) == 0 {
		return "\u6682\u65e0\u8fdb\u5c55\u8bb0\u5f55\u3002"
	}
	return strings.Join(formatted, "\r\n")
}

func historyMenuLabel(todo Todo) string {
	title := shortenText(todo.Title, 18, "\u672a\u547d\u540d\u4e8b\u9879")
	completedAt := parseStoredTime(todo.CompletedAt, parseStoredTime(todo.CreatedAt, time.Time{}))
	timeText := "\u672a\u8bb0\u5f55"
	if !completedAt.IsZero() {
		timeText = "\u5b8c\u6210 " + completedAt.Local().Format("01-02 15:04")
	}
	return fmt.Sprintf("%s \u00b7 %s", title, timeText)
}

func shortenText(value string, maxRunes int, fallback string) string {
	text := strings.TrimSpace(value)
	if text == "" {
		return fallback
	}
	runes := []rune(text)
	if maxRunes <= 0 || len(runes) <= maxRunes {
		return text
	}
	if maxRunes == 1 {
		return "\u2026"
	}
	return string(runes[:maxRunes-1]) + "\u2026"
}
