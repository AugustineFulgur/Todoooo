package main

import (
	"fmt"
	"strings"

	"github.com/lxn/walk"
)

type OverlayHeaderWidget struct {
	*walk.CustomWidget
	title        string
	subtitle     string
	countText    string
	onDrag       func()
	titleFont    *walk.Font
	subtitleFont *walk.Font
	countFont    *walk.Font
	height       int
}

func NewOverlayHeaderWidget(parent walk.Container, title, subtitle string, onDrag func()) (*OverlayHeaderWidget, error) {
	widget := &OverlayHeaderWidget{
		title:     title,
		subtitle:  subtitle,
		countText: "0 \u5f85\u5904\u7406",
		onDrag:    onDrag,
		height:    82,
	}

	cw, err := walk.NewCustomWidgetPixels(parent, 0, func(canvas *walk.Canvas, updateBounds walk.Rectangle) error {
		return widget.paint(canvas, updateBounds)
	})
	if err != nil {
		return nil, err
	}

	widget.CustomWidget = cw
	if err := walk.InitWrapperWindow(widget); err != nil {
		widget.Dispose()
		return nil, err
	}

	widget.SetBackground(walk.NullBrush())
	widget.SetPaintMode(walk.PaintBuffered)
	widget.SetInvalidatesOnResize(true)
	_ = widget.SetMinMaxSize(walk.Size{Width: 0, Height: widget.height}, walk.Size{Width: 0, Height: widget.height + 2})

	titleFont, err := walk.NewFont("Microsoft YaHei UI", 18, walk.FontBold)
	if err != nil {
		return nil, err
	}
	widget.AddDisposable(titleFont)
	widget.titleFont = titleFont

	subtitleFont, err := walk.NewFont("Microsoft YaHei UI", 9, 0)
	if err != nil {
		return nil, err
	}
	widget.AddDisposable(subtitleFont)
	widget.subtitleFont = subtitleFont

	countFont, err := walk.NewFont("Microsoft YaHei UI", 10, walk.FontBold)
	if err != nil {
		return nil, err
	}
	widget.AddDisposable(countFont)
	widget.countFont = countFont

	widget.MouseDown().Attach(func(x, y int, button walk.MouseButton) {
		if button == walk.LeftButton && widget.onDrag != nil {
			widget.onDrag()
		}
	})

	return widget, nil
}

func (w *OverlayHeaderWidget) CreateLayoutItem(ctx *walk.LayoutContext) walk.LayoutItem {
	return &overlayFixedLayoutItem{minHeight: w.height, idealHeight: w.height}
}

func (w *OverlayHeaderWidget) SetCountText(count int) {
	text := fmt.Sprintf("%d \u5f85\u5904\u7406", count)
	if text == w.countText {
		return
	}
	w.countText = text
	_ = w.Invalidate()
}

func (w *OverlayHeaderWidget) paint(canvas *walk.Canvas, _ walk.Rectangle) error {
	bounds := w.ClientBoundsPixels()
	paintOverlayBackground(canvas, bounds)
	scale := overlayScale(bounds.Width, 420)
	s := func(v int) int { return overlayMetric(v, scale) }

	panelBounds := walk.Rectangle{X: 0, Y: s(5), Width: bounds.Width - 1, Height: bounds.Height - s(10)}
	fillRoundedRectOnCanvas(canvas, rgb(239, 244, 249), rgb(239, 244, 249), walk.Rectangle{X: s(3), Y: s(9), Width: panelBounds.Width - s(6), Height: panelBounds.Height - s(2)}, s(22))
	fillRoundedRectOnCanvas(canvas, rgb(252, 254, 255), rgb(223, 228, 236), panelBounds, s(22))
	fillRoundedRectOnCanvas(canvas, rgb(43, 103, 162), rgb(43, 103, 162), walk.Rectangle{X: s(16), Y: s(17), Width: s(58), Height: s(5)}, s(3))
	fillRoundedRectOnCanvas(canvas, rgb(233, 241, 249), rgb(233, 241, 249), walk.Rectangle{X: s(16), Y: s(29), Width: s(90), Height: s(18)}, s(9))

	countWidth := clampInt(28+len([]rune(w.countText))*14, 96, 162)
	countBounds := walk.Rectangle{X: bounds.Width - countWidth - s(14), Y: s(17), Width: countWidth, Height: s(30)}
	fillRoundedRectOnCanvas(canvas, rgb(232, 240, 248), rgb(206, 219, 233), countBounds, s(15))
	_ = canvas.DrawTextPixels(w.countText, w.countFont, rgb(29, 61, 96), countBounds, walk.TextSingleLine|walk.TextCenter|walk.TextVCenter|walk.TextNoPrefix)

	_ = canvas.DrawTextPixels(w.title, w.titleFont, rgb(21, 28, 34), walk.Rectangle{X: s(16), Y: s(20), Width: bounds.Width - countWidth - s(48), Height: s(28)}, walk.TextSingleLine|walk.TextNoPrefix)
	_ = canvas.DrawTextPixels(w.subtitle, w.subtitleFont, rgb(92, 101, 111), walk.Rectangle{X: s(16), Y: s(51), Width: bounds.Width - s(28), Height: s(18)}, walk.TextSingleLine|walk.TextEndEllipsis|walk.TextNoPrefix)
	return nil
}

type OverlayDialogHeaderWidget struct {
	*walk.CustomWidget
	title        string
	subtitle     string
	onDrag       func()
	titleFont    *walk.Font
	subtitleFont *walk.Font
	height       int
}

func NewOverlayDialogHeaderWidget(parent walk.Container, title, subtitle string, onDrag func()) (*OverlayDialogHeaderWidget, error) {
	widget := &OverlayDialogHeaderWidget{
		title:    title,
		subtitle: subtitle,
		onDrag:   onDrag,
		height:   86,
	}

	cw, err := walk.NewCustomWidgetPixels(parent, 0, func(canvas *walk.Canvas, updateBounds walk.Rectangle) error {
		return widget.paint(canvas, updateBounds)
	})
	if err != nil {
		return nil, err
	}

	widget.CustomWidget = cw
	if err := walk.InitWrapperWindow(widget); err != nil {
		widget.Dispose()
		return nil, err
	}

	widget.SetBackground(walk.NullBrush())
	widget.SetPaintMode(walk.PaintBuffered)
	widget.SetInvalidatesOnResize(true)
	_ = widget.SetMinMaxSize(walk.Size{Width: 0, Height: widget.height}, walk.Size{Width: 0, Height: widget.height + 2})

	titleFont, err := walk.NewFont("Microsoft YaHei UI", 16, walk.FontBold)
	if err != nil {
		return nil, err
	}
	widget.AddDisposable(titleFont)
	widget.titleFont = titleFont

	subtitleFont, err := walk.NewFont("Microsoft YaHei UI", 9, 0)
	if err != nil {
		return nil, err
	}
	widget.AddDisposable(subtitleFont)
	widget.subtitleFont = subtitleFont

	widget.MouseDown().Attach(func(x, y int, button walk.MouseButton) {
		if button == walk.LeftButton && widget.onDrag != nil {
			widget.onDrag()
		}
	})

	return widget, nil
}

func (w *OverlayDialogHeaderWidget) CreateLayoutItem(ctx *walk.LayoutContext) walk.LayoutItem {
	return &overlayFixedLayoutItem{minHeight: w.height, idealHeight: w.height}
}

func (w *OverlayDialogHeaderWidget) paint(canvas *walk.Canvas, _ walk.Rectangle) error {
	bounds := w.ClientBoundsPixels()
	paintOverlayBackground(canvas, bounds)

	panelBounds := walk.Rectangle{X: 0, Y: 4, Width: bounds.Width - 1, Height: bounds.Height - 8}
	fillRoundedRectOnCanvas(canvas, rgb(239, 244, 249), rgb(239, 244, 249), walk.Rectangle{X: 3, Y: 8, Width: panelBounds.Width - 6, Height: panelBounds.Height - 2}, 22)
	fillRoundedRectOnCanvas(canvas, rgb(252, 254, 255), rgb(223, 228, 236), panelBounds, 22)
	fillRoundedRectOnCanvas(canvas, rgb(43, 103, 162), rgb(43, 103, 162), walk.Rectangle{X: 18, Y: 18, Width: 54, Height: 5}, 3)
	fillRoundedRectOnCanvas(canvas, rgb(233, 241, 249), rgb(233, 241, 249), walk.Rectangle{X: 18, Y: 32, Width: 84, Height: 18}, 9)

	_ = canvas.DrawTextPixels(w.title, w.titleFont, rgb(21, 28, 34), walk.Rectangle{X: 18, Y: 20, Width: bounds.Width - 36, Height: 28}, walk.TextSingleLine|walk.TextNoPrefix)
	_ = canvas.DrawTextPixels(w.subtitle, w.subtitleFont, rgb(92, 101, 111), walk.Rectangle{X: 18, Y: 52, Width: bounds.Width - 36, Height: 18}, walk.TextSingleLine|walk.TextEndEllipsis|walk.TextNoPrefix)
	return nil
}

type OverlayActionWidget struct {
	*walk.CustomWidget
	label    string
	onClick  func()
	textFont *walk.Font
	height   int
	pressed  bool
}

func NewOverlayActionWidget(parent walk.Container, label string, onClick func()) (*OverlayActionWidget, error) {
	widget := &OverlayActionWidget{
		label:   label,
		onClick: onClick,
		height:  42,
	}

	cw, err := walk.NewCustomWidgetPixels(parent, 0, func(canvas *walk.Canvas, updateBounds walk.Rectangle) error {
		return widget.paint(canvas, updateBounds)
	})
	if err != nil {
		return nil, err
	}

	widget.CustomWidget = cw
	if err := walk.InitWrapperWindow(widget); err != nil {
		widget.Dispose()
		return nil, err
	}

	widget.SetBackground(walk.NullBrush())
	widget.SetPaintMode(walk.PaintBuffered)
	widget.SetInvalidatesOnResize(true)
	_ = widget.SetMinMaxSize(walk.Size{Width: 0, Height: widget.height}, walk.Size{Width: 0, Height: widget.height + 2})

	textFont, err := walk.NewFont("Microsoft YaHei UI", 10, walk.FontBold)
	if err != nil {
		return nil, err
	}
	widget.AddDisposable(textFont)
	widget.textFont = textFont

	widget.MouseDown().Attach(func(x, y int, button walk.MouseButton) {
		if button != walk.LeftButton {
			return
		}
		widget.pressed = true
		_ = widget.Invalidate()
	})
	widget.MouseUp().Attach(func(x, y int, button walk.MouseButton) {
		if button != walk.LeftButton {
			return
		}
		wasPressed := widget.pressed
		widget.pressed = false
		_ = widget.Invalidate()
		if wasPressed && pointInRect(x, y, widget.ClientBoundsPixels()) && widget.onClick != nil {
			widget.onClick()
		}
	})

	return widget, nil
}

func (w *OverlayActionWidget) CreateLayoutItem(ctx *walk.LayoutContext) walk.LayoutItem {
	return &overlayFixedLayoutItem{minHeight: w.height, idealHeight: w.height}
}

func (w *OverlayActionWidget) paint(canvas *walk.Canvas, _ walk.Rectangle) error {
	bounds := w.ClientBoundsPixels()
	paintOverlayBackground(canvas, bounds)
	scale := overlayScale(bounds.Width, 420)
	s := func(v int) int { return overlayMetric(v, scale) }

	frameBounds := walk.Rectangle{X: 0, Y: 0, Width: bounds.Width - 1, Height: bounds.Height - 1}
	fillRoundedRectOnCanvas(canvas, rgb(252, 254, 255), rgb(223, 228, 236), frameBounds, s(16))

	buttonBounds := insetRect(frameBounds, s(8), s(6))
	fill := rgb(43, 103, 162)
	border := rgb(43, 103, 162)
	if w.pressed {
		fill = rgb(35, 90, 147)
		border = rgb(35, 90, 147)
	}

	fillRoundedRectOnCanvas(canvas, fill, border, buttonBounds, s(12))
	_ = canvas.DrawTextPixels(w.label, w.textFont, rgb(255, 255, 255), buttonBounds, walk.TextSingleLine|walk.TextCenter|walk.TextVCenter|walk.TextNoPrefix)
	return nil
}

type OverlayMiniActionWidget struct {
	*walk.CustomWidget
	label    string
	onClick  func()
	textFont *walk.Font
	width    int
	height   int
	pressed  bool
}

func NewOverlayMiniActionWidget(parent walk.Container, label string, onClick func()) (*OverlayMiniActionWidget, error) {
	widget := &OverlayMiniActionWidget{
		label:   label,
		onClick: onClick,
		width:   120,
		height:  36,
	}

	cw, err := walk.NewCustomWidgetPixels(parent, 0, func(canvas *walk.Canvas, updateBounds walk.Rectangle) error {
		return widget.paint(canvas, updateBounds)
	})
	if err != nil {
		return nil, err
	}

	widget.CustomWidget = cw
	if err := walk.InitWrapperWindow(widget); err != nil {
		widget.Dispose()
		return nil, err
	}

	widget.SetBackground(walk.NullBrush())
	widget.SetPaintMode(walk.PaintBuffered)
	widget.SetInvalidatesOnResize(true)
	_ = widget.SetMinMaxSize(
		walk.Size{Width: widget.width, Height: widget.height},
		walk.Size{Width: widget.width, Height: widget.height},
	)

	textFont, err := walk.NewFont("Microsoft YaHei UI", 9, walk.FontBold)
	if err != nil {
		return nil, err
	}
	widget.AddDisposable(textFont)
	widget.textFont = textFont

	widget.MouseDown().Attach(func(x, y int, button walk.MouseButton) {
		if button != walk.LeftButton {
			return
		}
		widget.pressed = true
		_ = widget.Invalidate()
	})
	widget.MouseUp().Attach(func(x, y int, button walk.MouseButton) {
		if button != walk.LeftButton {
			return
		}
		wasPressed := widget.pressed
		widget.pressed = false
		_ = widget.Invalidate()
		if wasPressed && pointInRect(x, y, widget.ClientBoundsPixels()) && widget.onClick != nil {
			widget.onClick()
		}
	})

	return widget, nil
}

func (w *OverlayMiniActionWidget) SetLabel(label string) {
	label = strings.TrimSpace(label)
	if label == "" {
		return
	}
	if label == w.label {
		return
	}
	w.label = label
	_ = w.Invalidate()
}

func (w *OverlayMiniActionWidget) CreateLayoutItem(ctx *walk.LayoutContext) walk.LayoutItem {
	return &overlayFixedWidthLayoutItem{width: w.width, height: w.height}
}

func (w *OverlayMiniActionWidget) paint(canvas *walk.Canvas, _ walk.Rectangle) error {
	bounds := w.ClientBoundsPixels()
	paintOverlayBackground(canvas, bounds)

	frameBounds := walk.Rectangle{X: 0, Y: 0, Width: bounds.Width - 1, Height: bounds.Height - 1}
	fill := rgb(252, 254, 255)
	border := rgb(223, 228, 236)
	textColor := rgb(76, 88, 100)
	if w.pressed {
		fill = rgb(244, 249, 253)
		border = rgb(209, 220, 232)
	}

	fillRoundedRectOnCanvas(canvas, fill, border, frameBounds, 12)
	_ = canvas.DrawTextPixels(w.label, w.textFont, textColor, frameBounds, walk.TextSingleLine|walk.TextCenter|walk.TextVCenter|walk.TextNoPrefix)
	return nil
}

type OverlayDialogActionWidget struct {
	*walk.CustomWidget
	label    string
	primary  bool
	onClick  func()
	textFont *walk.Font
	width    int
	height   int
	pressed  bool
}

func NewOverlayDialogActionWidget(parent walk.Container, label string, primary bool, onClick func()) (*OverlayDialogActionWidget, error) {
	width := 122
	if primary {
		width = 132
	}
	widget := &OverlayDialogActionWidget{
		label:   label,
		primary: primary,
		onClick: onClick,
		width:   width,
		height:  42,
	}

	cw, err := walk.NewCustomWidgetPixels(parent, 0, func(canvas *walk.Canvas, updateBounds walk.Rectangle) error {
		return widget.paint(canvas, updateBounds)
	})
	if err != nil {
		return nil, err
	}

	widget.CustomWidget = cw
	if err := walk.InitWrapperWindow(widget); err != nil {
		widget.Dispose()
		return nil, err
	}

	widget.SetBackground(walk.NullBrush())
	widget.SetPaintMode(walk.PaintBuffered)
	widget.SetInvalidatesOnResize(true)
	_ = widget.SetMinMaxSize(
		walk.Size{Width: widget.width, Height: widget.height},
		walk.Size{Width: widget.width, Height: widget.height},
	)

	textFont, err := walk.NewFont("Microsoft YaHei UI", 10, walk.FontBold)
	if err != nil {
		return nil, err
	}
	widget.AddDisposable(textFont)
	widget.textFont = textFont

	widget.MouseDown().Attach(func(x, y int, button walk.MouseButton) {
		if button != walk.LeftButton {
			return
		}
		widget.pressed = true
		_ = widget.Invalidate()
	})
	widget.MouseUp().Attach(func(x, y int, button walk.MouseButton) {
		if button != walk.LeftButton {
			return
		}
		wasPressed := widget.pressed
		widget.pressed = false
		_ = widget.Invalidate()
		if wasPressed && pointInRect(x, y, widget.ClientBoundsPixels()) && widget.onClick != nil {
			widget.onClick()
		}
	})

	return widget, nil
}

func (w *OverlayDialogActionWidget) CreateLayoutItem(ctx *walk.LayoutContext) walk.LayoutItem {
	return &overlayFixedWidthLayoutItem{width: w.width, height: w.height}
}

func (w *OverlayDialogActionWidget) paint(canvas *walk.Canvas, _ walk.Rectangle) error {
	bounds := w.ClientBoundsPixels()
	paintOverlayBackground(canvas, bounds)

	frameBounds := walk.Rectangle{X: 0, Y: 0, Width: bounds.Width - 1, Height: bounds.Height - 1}
	fill := rgb(252, 254, 255)
	border := rgb(223, 228, 236)
	textColor := rgb(76, 88, 100)
	if w.primary {
		fill = rgb(43, 103, 162)
		border = rgb(43, 103, 162)
		textColor = rgb(255, 255, 255)
	}
	if w.pressed {
		if w.primary {
			fill = rgb(35, 90, 147)
			border = rgb(35, 90, 147)
		} else {
			fill = rgb(244, 249, 253)
			border = rgb(209, 220, 232)
		}
	}

	fillRoundedRectOnCanvas(canvas, fill, border, frameBounds, 14)
	_ = canvas.DrawTextPixels(w.label, w.textFont, textColor, frameBounds, walk.TextSingleLine|walk.TextCenter|walk.TextVCenter|walk.TextNoPrefix)
	return nil
}

type OverlayCollapseWidget struct {
	*walk.CustomWidget
	onClick  func()
	iconFont *walk.Font
	height   int
	width    int
	pressed  bool
}

func NewOverlayCollapseWidget(parent walk.Container, onClick func()) (*OverlayCollapseWidget, error) {
	widget := &OverlayCollapseWidget{
		onClick: onClick,
		height:  74,
		width:   42,
	}

	cw, err := walk.NewCustomWidgetPixels(parent, 0, func(canvas *walk.Canvas, updateBounds walk.Rectangle) error {
		return widget.paint(canvas, updateBounds)
	})
	if err != nil {
		return nil, err
	}

	widget.CustomWidget = cw
	if err := walk.InitWrapperWindow(widget); err != nil {
		widget.Dispose()
		return nil, err
	}

	widget.SetBackground(walk.NullBrush())
	widget.SetPaintMode(walk.PaintBuffered)
	widget.SetInvalidatesOnResize(true)
	_ = widget.SetMinMaxSize(
		walk.Size{Width: widget.width, Height: widget.height},
		walk.Size{Width: widget.width, Height: widget.height},
	)

	iconFont, err := walk.NewFont("Microsoft YaHei UI", 11, walk.FontBold)
	if err != nil {
		return nil, err
	}
	widget.AddDisposable(iconFont)
	widget.iconFont = iconFont

	widget.MouseDown().Attach(func(x, y int, button walk.MouseButton) {
		if button != walk.LeftButton {
			return
		}
		widget.pressed = true
		_ = widget.Invalidate()
	})
	widget.MouseUp().Attach(func(x, y int, button walk.MouseButton) {
		if button != walk.LeftButton {
			return
		}
		wasPressed := widget.pressed
		widget.pressed = false
		_ = widget.Invalidate()
		if wasPressed && pointInRect(x, y, widget.ClientBoundsPixels()) && widget.onClick != nil {
			widget.onClick()
		}
	})

	return widget, nil
}

func (w *OverlayCollapseWidget) CreateLayoutItem(ctx *walk.LayoutContext) walk.LayoutItem {
	return &overlayFixedWidthLayoutItem{width: w.width, height: w.height}
}

func (w *OverlayCollapseWidget) paint(canvas *walk.Canvas, _ walk.Rectangle) error {
	bounds := w.ClientBoundsPixels()
	paintOverlayBackground(canvas, bounds)
	scale := overlayScale(bounds.Width, 42)
	s := func(v int) int { return overlayMetric(v, scale) }

	cardBounds := walk.Rectangle{X: 0, Y: s(6), Width: bounds.Width - 1, Height: bounds.Height - s(12)}
	fillRoundedRectOnCanvas(canvas, rgb(239, 244, 249), rgb(239, 244, 249), walk.Rectangle{X: s(3), Y: s(10), Width: cardBounds.Width - s(6), Height: cardBounds.Height - s(2)}, s(20))
	fill := rgb(252, 254, 255)
	border := rgb(223, 228, 236)
	iconBounds := insetRect(cardBounds, s(8), s(12))
	iconFill := rgb(236, 242, 249)
	iconBorder := rgb(220, 229, 239)
	if w.pressed {
		fill = rgb(245, 249, 253)
		border = rgb(210, 221, 232)
		iconFill = rgb(226, 236, 246)
		iconBorder = rgb(205, 220, 234)
	}

	fillRoundedRectOnCanvas(canvas, fill, border, cardBounds, s(18))
	fillRoundedRectOnCanvas(canvas, iconFill, iconBorder, iconBounds, s(12))
	_ = canvas.DrawTextPixels("\u25b6", w.iconFont, rgb(64, 87, 114), iconBounds, walk.TextSingleLine|walk.TextCenter|walk.TextVCenter|walk.TextNoPrefix)
	return nil
}

type OverlaySortWidget struct {
	*walk.CustomWidget
	selected        SortMode
	expanded        bool
	onChange        func(SortMode)
	onLayoutChanged func()
	labelFont       *walk.Font
	valueFont       *walk.Font
	optionFont      *walk.Font
	rowHeight       int
	expandedHeight  int
	pressedHeader   bool
	pressedOption   SortMode
}

func NewOverlaySortWidget(parent walk.Container, selected SortMode, onChange func(SortMode), onLayoutChanged func()) (*OverlaySortWidget, error) {
	widget := &OverlaySortWidget{
		selected:        selected,
		onChange:        onChange,
		onLayoutChanged: onLayoutChanged,
		rowHeight:       40,
		expandedHeight:  92,
	}

	cw, err := walk.NewCustomWidgetPixels(parent, 0, func(canvas *walk.Canvas, updateBounds walk.Rectangle) error {
		return widget.paint(canvas, updateBounds)
	})
	if err != nil {
		return nil, err
	}

	widget.CustomWidget = cw
	if err := walk.InitWrapperWindow(widget); err != nil {
		widget.Dispose()
		return nil, err
	}

	widget.SetBackground(walk.NullBrush())
	widget.SetPaintMode(walk.PaintBuffered)
	widget.SetInvalidatesOnResize(true)

	labelFont, err := walk.NewFont("Microsoft YaHei UI", 9, 0)
	if err != nil {
		return nil, err
	}
	widget.AddDisposable(labelFont)
	widget.labelFont = labelFont

	valueFont, err := walk.NewFont("Microsoft YaHei UI", 9, walk.FontBold)
	if err != nil {
		return nil, err
	}
	widget.AddDisposable(valueFont)
	widget.valueFont = valueFont

	optionFont, err := walk.NewFont("Microsoft YaHei UI", 8, walk.FontBold)
	if err != nil {
		return nil, err
	}
	widget.AddDisposable(optionFont)
	widget.optionFont = optionFont

	widget.MouseDown().Attach(func(x, y int, button walk.MouseButton) {
		if button != walk.LeftButton {
			return
		}
		switch area := widget.hitTest(x, y); area {
		case "header":
			widget.pressedHeader = true
		case string(SortModePriority):
			widget.pressedOption = SortModePriority
		case string(SortModeDeadline):
			widget.pressedOption = SortModeDeadline
		}
		_ = widget.Invalidate()
	})
	widget.MouseUp().Attach(func(x, y int, button walk.MouseButton) {
		if button != walk.LeftButton {
			return
		}
		area := widget.hitTest(x, y)
		pressedHeader := widget.pressedHeader
		pressedOption := widget.pressedOption
		widget.pressedHeader = false
		widget.pressedOption = ""
		_ = widget.Invalidate()

		if pressedHeader && area == "header" {
			widget.toggleExpanded()
			return
		}
		if pressedOption != "" && area == string(pressedOption) {
			widget.selectMode(pressedOption)
		}
	})

	return widget, nil
}

func (w *OverlaySortWidget) CreateLayoutItem(ctx *walk.LayoutContext) walk.LayoutItem {
	return &overlayDynamicLayoutItem{heightFunc: w.CurrentHeight}
}

func (w *OverlaySortWidget) CurrentHeight() int {
	if w.expanded {
		return w.expandedHeight
	}
	return w.rowHeight
}

func (w *OverlaySortWidget) SetSelected(mode SortMode) {
	if mode == "" || mode == w.selected {
		return
	}
	w.selected = mode
	_ = w.Invalidate()
}

func (w *OverlaySortWidget) paint(canvas *walk.Canvas, _ walk.Rectangle) error {
	bounds := w.ClientBoundsPixels()
	paintOverlayBackground(canvas, bounds)
	scale := overlayScale(bounds.Width, 420)
	s := func(v int) int { return overlayMetric(v, scale) }

	headerBounds := walk.Rectangle{X: 0, Y: 0, Width: bounds.Width - 1, Height: w.rowHeight - s(2)}
	fill := rgb(252, 254, 255)
	border := rgb(223, 228, 236)
	if w.pressedHeader {
		fill = rgb(244, 249, 253)
		border = rgb(209, 220, 232)
	}
	fillRoundedRectOnCanvas(canvas, fill, border, headerBounds, s(14))

	_ = canvas.DrawTextPixels("\u6392\u5e8f\u65b9\u5f0f", w.labelFont, rgb(90, 100, 111), walk.Rectangle{X: s(14), Y: 0, Width: s(94), Height: w.rowHeight - s(2)}, walk.TextSingleLine|walk.TextVCenter|walk.TextNoPrefix)

	selectedWidth := clampInt(bounds.Width-s(154), s(128), s(176))
	selectedBounds := walk.Rectangle{X: bounds.Width - selectedWidth - s(36), Y: s(6), Width: selectedWidth, Height: s(26)}
	fillRoundedRectOnCanvas(canvas, rgb(234, 241, 248), rgb(210, 221, 233), selectedBounds, s(13))
	_ = canvas.DrawTextPixels(sortModeLabel(w.selected), w.valueFont, rgb(33, 62, 94), walk.Rectangle{X: selectedBounds.X + s(12), Y: selectedBounds.Y, Width: selectedBounds.Width - s(26), Height: selectedBounds.Height}, walk.TextSingleLine|walk.TextVCenter|walk.TextNoPrefix)
	_ = canvas.DrawTextPixels(sortChevron(w.expanded), w.valueFont, rgb(86, 99, 111), walk.Rectangle{X: bounds.Width - s(34), Y: 0, Width: s(20), Height: w.rowHeight - s(2)}, walk.TextSingleLine|walk.TextCenter|walk.TextVCenter|walk.TextNoPrefix)

	if !w.expanded {
		return nil
	}

	panelBounds := walk.Rectangle{X: 0, Y: s(44), Width: bounds.Width - 1, Height: bounds.Height - s(45)}
	fillRoundedRectOnCanvas(canvas, rgb(252, 254, 255), rgb(223, 228, 236), panelBounds, s(16))

	_ = canvas.DrawTextPixels("\u5feb\u6377\u6392\u5e8f", w.labelFont, rgb(104, 112, 120), walk.Rectangle{X: s(14), Y: s(48), Width: s(90), Height: s(18)}, walk.TextSingleLine|walk.TextVCenter|walk.TextNoPrefix)

	for _, mode := range []SortMode{SortModePriority, SortModeDeadline} {
		optionBounds := w.optionBounds(mode)
		selected := w.selected == mode
		pressed := w.pressedOption == mode
		fillColor := rgb(247, 250, 253)
		borderColor := rgb(223, 228, 236)
		textColor := rgb(80, 89, 98)
		if selected {
			fillColor = rgb(233, 241, 248)
			borderColor = rgb(205, 220, 234)
			textColor = rgb(32, 61, 94)
		}
		if pressed {
			fillColor = rgb(223, 234, 245)
			borderColor = rgb(194, 211, 228)
		}
		fillRoundedRectOnCanvas(canvas, fillColor, borderColor, optionBounds, 14)
		_ = canvas.DrawTextPixels(sortModeOptionLabel(mode), w.optionFont, textColor, optionBounds, walk.TextSingleLine|walk.TextCenter|walk.TextVCenter|walk.TextNoPrefix)
	}

	return nil
}

func (w *OverlaySortWidget) hitTest(x, y int) string {
	if pointInRect(x, y, walk.Rectangle{X: 0, Y: 0, Width: w.ClientBoundsPixels().Width, Height: w.rowHeight}) {
		return "header"
	}
	if !w.expanded {
		return ""
	}
	for _, mode := range []SortMode{SortModePriority, SortModeDeadline} {
		if pointInRect(x, y, w.optionBounds(mode)) {
			return string(mode)
		}
	}
	return ""
}

func (w *OverlaySortWidget) optionBounds(mode SortMode) walk.Rectangle {
	bounds := w.ClientBoundsPixels()
	baseY := 64
	width := (bounds.Width - 44) / 2
	if width < 118 {
		width = 118
	}
	switch mode {
	case SortModePriority:
		return walk.Rectangle{X: 14, Y: baseY, Width: width, Height: 28}
	default:
		return walk.Rectangle{X: bounds.Width - width - 14, Y: baseY, Width: width, Height: 28}
	}
}

func (w *OverlaySortWidget) toggleExpanded() {
	w.expanded = !w.expanded
	w.RequestLayout()
	_ = w.Invalidate()
	if w.onLayoutChanged != nil {
		w.onLayoutChanged()
	}
}

func (w *OverlaySortWidget) selectMode(mode SortMode) {
	changed := w.selected != mode
	w.selected = mode
	w.expanded = false
	w.RequestLayout()
	_ = w.Invalidate()
	if changed && w.onChange != nil {
		w.onChange(mode)
	}
	if w.onLayoutChanged != nil {
		w.onLayoutChanged()
	}
}

type DockTabWidget struct {
	*walk.CustomWidget
	countText string
	onClick   func()
	countFont *walk.Font
	width     int
	height    int
	pressed   bool
}

func NewDockTabWidget(parent walk.Container, onClick func()) (*DockTabWidget, error) {
	widget := &DockTabWidget{
		countText: "0",
		onClick:   onClick,
		width:     54,
		height:    54,
	}

	cw, err := walk.NewCustomWidgetPixels(parent, 0, func(canvas *walk.Canvas, updateBounds walk.Rectangle) error {
		return widget.paint(canvas, updateBounds)
	})
	if err != nil {
		return nil, err
	}

	widget.CustomWidget = cw
	if err := walk.InitWrapperWindow(widget); err != nil {
		widget.Dispose()
		return nil, err
	}

	widget.SetBackground(walk.NullBrush())
	widget.SetPaintMode(walk.PaintBuffered)
	widget.SetInvalidatesOnResize(true)
	_ = widget.SetMinMaxSize(
		walk.Size{Width: widget.width, Height: widget.height},
		walk.Size{Width: widget.width, Height: widget.height},
	)

	countFont, err := walk.NewFont("Microsoft YaHei UI", 11, walk.FontBold)
	if err != nil {
		return nil, err
	}
	widget.AddDisposable(countFont)
	widget.countFont = countFont

	widget.MouseDown().Attach(func(x, y int, button walk.MouseButton) {
		if button != walk.LeftButton {
			return
		}
		widget.pressed = true
		_ = widget.Invalidate()
	})
	widget.MouseUp().Attach(func(x, y int, button walk.MouseButton) {
		if button != walk.LeftButton {
			return
		}
		wasPressed := widget.pressed
		widget.pressed = false
		_ = widget.Invalidate()
		if wasPressed && pointInRect(x, y, widget.ClientBoundsPixels()) && widget.onClick != nil {
			widget.onClick()
		}
	})

	return widget, nil
}

func (w *DockTabWidget) CreateLayoutItem(ctx *walk.LayoutContext) walk.LayoutItem {
	return &overlayFixedWidthLayoutItem{width: w.width, height: w.height}
}

func (w *DockTabWidget) SetCountText(count int) {
	text := fmt.Sprintf("%d", count)
	if count > 99 {
		text = "99+"
	}
	if text == w.countText {
		return
	}
	w.countText = text
	_ = w.Invalidate()
}

func (w *DockTabWidget) paint(canvas *walk.Canvas, _ walk.Rectangle) error {
	bounds := w.ClientBoundsPixels()
	paintOverlayBackground(canvas, bounds)
	scale := overlayScale(bounds.Width, 54)
	s := func(v int) int { return overlayMetric(v, scale) }

	cardBounds := walk.Rectangle{X: s(3), Y: s(3), Width: bounds.Width - s(6), Height: bounds.Height - s(6)}
	shadowBounds := walk.Rectangle{X: s(5), Y: s(5), Width: cardBounds.Width, Height: cardBounds.Height}
	fill := rgb(252, 254, 255)
	border := rgb(214, 223, 234)
	textColor := rgb(29, 61, 96)
	if w.pressed {
		fill = rgb(244, 249, 253)
		border = rgb(199, 213, 228)
	}

	fillRoundedRectOnCanvas(canvas, rgb(236, 242, 248), rgb(236, 242, 248), shadowBounds, s(24))
	fillRoundedRectOnCanvas(canvas, fill, border, cardBounds, s(24))
	_ = canvas.DrawTextPixels(w.countText, w.countFont, textColor, cardBounds, walk.TextSingleLine|walk.TextCenter|walk.TextVCenter|walk.TextNoPrefix)
	return nil
}

type overlayFixedLayoutItem struct {
	walk.LayoutItemBase
	minHeight   int
	idealHeight int
}

func (*overlayFixedLayoutItem) LayoutFlags() walk.LayoutFlags {
	return walk.GrowableHorz | walk.GreedyHorz
}

func (li *overlayFixedLayoutItem) IdealSize() walk.Size {
	return walk.Size{Width: 200, Height: li.idealHeight}
}

func (li *overlayFixedLayoutItem) MinSize() walk.Size {
	return walk.Size{Width: 120, Height: li.minHeight}
}

type overlayFixedWidthLayoutItem struct {
	walk.LayoutItemBase
	width  int
	height int
}

func (*overlayFixedWidthLayoutItem) LayoutFlags() walk.LayoutFlags {
	return walk.ShrinkableHorz
}

func (li *overlayFixedWidthLayoutItem) IdealSize() walk.Size {
	return walk.Size{Width: li.width, Height: li.height}
}

func (li *overlayFixedWidthLayoutItem) MinSize() walk.Size {
	return walk.Size{Width: li.width, Height: li.height}
}

type overlayDynamicLayoutItem struct {
	walk.LayoutItemBase
	heightFunc func() int
}

func (*overlayDynamicLayoutItem) LayoutFlags() walk.LayoutFlags {
	return walk.GrowableHorz | walk.GreedyHorz
}

func (li *overlayDynamicLayoutItem) IdealSize() walk.Size {
	return walk.Size{Width: 200, Height: li.heightFunc()}
}

func (li *overlayDynamicLayoutItem) MinSize() walk.Size {
	return walk.Size{Width: 120, Height: li.heightFunc()}
}

func paintOverlayBackground(canvas *walk.Canvas, bounds walk.Rectangle) {
	fillRoundedRectOnCanvas(canvas, rgb(245, 247, 251), rgb(245, 247, 251), walk.Rectangle{X: 0, Y: 0, Width: bounds.Width, Height: bounds.Height}, 0)
}

func insetRect(bounds walk.Rectangle, insetX, insetY int) walk.Rectangle {
	bounds.X += insetX
	bounds.Y += insetY
	bounds.Width -= insetX * 2
	bounds.Height -= insetY * 2
	if bounds.Width < 0 {
		bounds.Width = 0
	}
	if bounds.Height < 0 {
		bounds.Height = 0
	}
	return bounds
}

func fillRoundedRectOnCanvas(canvas *walk.Canvas, fill, stroke walk.Color, bounds walk.Rectangle, radius int) {
	brush, err := walk.NewSolidColorBrush(fill)
	if err != nil {
		return
	}
	defer brush.Dispose()

	pen, err := walk.NewCosmeticPen(walk.PenSolid, stroke)
	if err != nil {
		return
	}
	defer pen.Dispose()

	_ = canvas.FillRoundedRectanglePixels(brush, bounds, walk.Size{Width: radius, Height: radius})
	_ = canvas.DrawRoundedRectanglePixels(pen, bounds, walk.Size{Width: radius, Height: radius})
}

func overlayScale(width int, base int) float64 {
	if base <= 0 {
		return 1
	}
	scale := float64(width) / float64(base)
	if scale < 0.85 {
		return 0.85
	}
	if scale > 1.25 {
		return 1.25
	}
	return scale
}

func overlayMetric(value int, scale float64) int {
	scaled := int(float64(value)*scale + 0.5)
	if scaled < 1 {
		return 1
	}
	return scaled
}
