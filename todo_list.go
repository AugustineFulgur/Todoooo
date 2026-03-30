package main

import (
	"math"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/lxn/walk"
)

const (
	boardPaddingX        = 8
	boardPaddingY        = 8
	cardSpacing          = 14
	cardRadius           = 20
	cardMinHeight        = 114
	cardHorizontalInset  = 20
	cardTopInset         = 18
	cardBottomInset      = 18
	pillHeight           = 24
	pillGap              = 8
	metaRowGap           = 6
	metaToTitleGap       = 12
	titleToBodyGap       = 8
	checkSize            = 30
	checkAreaWidth       = 88
	postponeButtonWidth  = 58
	postponeButtonHeight = 26
	emptyStateHeight     = 120
	minBoardWidth        = 360
	maxBodyHeight        = 76
)

type cardLayout struct {
	todo            Todo
	bodyText        string
	actionLabel    string
	cardBounds      walk.Rectangle
	accentBounds    walk.Rectangle
	typeBounds      walk.Rectangle
	deadlineBounds  walk.Rectangle
	countdownBounds walk.Rectangle
	tagBounds       walk.Rectangle
	titleBounds     walk.Rectangle
	detailsBounds   walk.Rectangle
	checkBounds     walk.Rectangle
	postponeBounds  walk.Rectangle
	height          int
}

type CardPainter struct {
	measureCanvas   *walk.Canvas
	titleFont       *walk.Font
	metaFont        *walk.Font
	bodyFont        *walk.Font
	urgencyTintDays int
	measureMu       sync.Mutex
}

func NewCardPainter(owner walk.Window) (*CardPainter, error) {
	bitmap, err := walk.NewBitmapForDPI(walk.Size{Width: 8, Height: 8}, owner.DPI())
	if err != nil {
		return nil, err
	}
	owner.AddDisposable(bitmap)

	canvas, err := walk.NewCanvasFromImage(bitmap)
	if err != nil {
		return nil, err
	}
	owner.AddDisposable(canvas)

	titleFont, err := walk.NewFont("Microsoft YaHei UI", 10, walk.FontBold)
	if err != nil {
		return nil, err
	}
	owner.AddDisposable(titleFont)

	metaFont, err := walk.NewFont("Microsoft YaHei UI", 8, 0)
	if err != nil {
		return nil, err
	}
	owner.AddDisposable(metaFont)

	bodyFont, err := walk.NewFont("Microsoft YaHei UI", 9, 0)
	if err != nil {
		return nil, err
	}
	owner.AddDisposable(bodyFont)

	return &CardPainter{
		measureCanvas:   canvas,
		titleFont:       titleFont,
		metaFont:        metaFont,
		bodyFont:        bodyFont,
		urgencyTintDays: 15,
	}, nil
}

func (p *CardPainter) SetUrgencyTintDays(days int) {
	if days <= 0 {
		days = 15
	}
	if days > 365 {
		days = 365
	}
	p.urgencyTintDays = days
}

func (p *CardPainter) measurePillWidth(text string) int {
	if strings.TrimSpace(text) == "" {
		return 72
	}

	width := 24
	for _, r := range text {
		switch {
		case r <= 127:
			width += 7
		case r == ' ':
			width += 4
		default:
			width += 14
		}
	}

	return clampInt(width, 72, 210)
}

func (p *CardPainter) measureTextHeight(text string, font *walk.Font, width, maxHeight int) int {
	if strings.TrimSpace(text) == "" {
		return 0
	}

	p.measureMu.Lock()
	defer p.measureMu.Unlock()

	bounds, _, err := p.measureCanvas.MeasureTextPixels(
		text,
		font,
		walk.Rectangle{Width: width, Height: maxHeight},
		walk.TextWordbreak|walk.TextEditControl|walk.TextNoPrefix,
	)
	if err != nil {
		return 0
	}

	if bounds.Height > maxHeight {
		return maxHeight
	}

	return bounds.Height
}

func (p *CardPainter) layoutTodo(todo Todo, width, y int, actionLabel string) cardLayout {
	if width < minBoardWidth {
		width = minBoardWidth
	}

	availableWidth := width - boardPaddingX*2
	cardWidth := int(math.Round(float64(width) * 0.90))
	if cardWidth > availableWidth {
		cardWidth = availableWidth
	}
	if cardWidth < 320 {
		cardWidth = 320
	}

	cardX := (width - cardWidth) / 2
	if cardX < boardPaddingX {
		cardX = boardPaddingX
	}
	textWidth := cardWidth - cardHorizontalInset*2 - checkAreaWidth
	if textWidth < 220 {
		textWidth = 220
	}

	typeText := string(todo.Kind)
	deadlineText := "\u622a\u6b62 " + formatTodoDeadline(todo.Deadline)
	countdownText := deadlineCountdownLabel(todo.Deadline)
	tagText := firstTag(todo.Tags)
	typeWidth := p.measurePillWidth(typeText)
	deadlineWidth := p.measurePillWidth(deadlineText)
	countdownWidth := p.measurePillWidth(countdownText)
	tagWidth := p.measurePillWidth(tagText)

	contentX := cardX + cardHorizontalInset
	contentY := y + cardTopInset + 10

	metaHeight := pillHeight
	countdownX := contentX + typeWidth + pillGap + deadlineWidth + pillGap
	countdownY := contentY
	tagX := countdownX + countdownWidth + pillGap
	tagY := contentY
	if typeWidth+pillGap+deadlineWidth+pillGap+countdownWidth+pillGap+tagWidth > textWidth {
		metaHeight = pillHeight*2 + metaRowGap
		countdownX = contentX
		countdownY = contentY + pillHeight + metaRowGap
		tagX = countdownX + countdownWidth + pillGap
		tagY = countdownY
	}

	titleHeight := p.measureTextHeight(todo.Title, p.titleFont, textWidth, 56)
	if titleHeight < 26 {
		titleHeight = 26
	}

	bodyText := cardBodyText(todo)
	detailsHeight := p.measureTextHeight(bodyText, p.bodyFont, textWidth, maxBodyHeight)

	height := cardTopInset + 8 + metaHeight + metaToTitleGap + titleHeight + cardBottomInset
	if detailsHeight > 0 {
		height += titleToBodyGap + detailsHeight
	}
	if height < cardMinHeight {
		height = cardMinHeight
	}

	cardBounds := walk.Rectangle{X: cardX, Y: y, Width: cardWidth, Height: height}
	accentBounds := walk.Rectangle{X: cardBounds.X + 16, Y: cardBounds.Y + 12, Width: cardBounds.Width - 32, Height: 5}
	contentX = cardBounds.X + cardHorizontalInset
	contentY = cardBounds.Y + cardTopInset + 10

	typeBounds := walk.Rectangle{X: contentX, Y: contentY, Width: typeWidth, Height: pillHeight}
	deadlineBounds := walk.Rectangle{X: typeBounds.X + typeBounds.Width + pillGap, Y: contentY, Width: deadlineWidth, Height: pillHeight}
	countdownBounds := walk.Rectangle{X: countdownX, Y: countdownY, Width: countdownWidth, Height: pillHeight}
	tagBounds := walk.Rectangle{X: tagX, Y: tagY, Width: tagWidth, Height: pillHeight}
	titleY := contentY + metaHeight + metaToTitleGap
	titleBounds := walk.Rectangle{X: contentX, Y: titleY, Width: textWidth, Height: titleHeight}
	detailsBounds := walk.Rectangle{X: contentX, Y: titleY + titleHeight + titleToBodyGap, Width: textWidth, Height: detailsHeight}
	actionColumnWidth := maxInt(checkSize, postponeButtonWidth)
	actionColumnX := cardBounds.X + cardBounds.Width - cardHorizontalInset - actionColumnWidth
	checkBounds := walk.Rectangle{
		X:      actionColumnX + (actionColumnWidth-checkSize)/2,
		Y:      cardBounds.Y + (cardBounds.Height-checkSize)/2,
		Width:  checkSize,
		Height: checkSize,
	}
	postponeBounds := walk.Rectangle{
		X:      actionColumnX + (actionColumnWidth-postponeButtonWidth)/2,
		Y:      cardBounds.Y + cardBounds.Height - cardBottomInset - postponeButtonHeight + 2,
		Width:  postponeButtonWidth,
		Height: postponeButtonHeight,
	}

	return cardLayout{
		todo:            todo,
		bodyText:        bodyText,
		actionLabel:    actionLabel,
		cardBounds:      cardBounds,
		accentBounds:    accentBounds,
		typeBounds:      typeBounds,
		deadlineBounds:  deadlineBounds,
		countdownBounds: countdownBounds,
		tagBounds:       tagBounds,
		titleBounds:     titleBounds,
		detailsBounds:   detailsBounds,
		checkBounds:     checkBounds,
		postponeBounds:  postponeBounds,
		height:          height,
	}
}

func (p *CardPainter) drawCard(canvas *walk.Canvas, layout cardLayout, offset int, fade float64, removing bool, showCheck bool, showAction bool) {
	cardBounds := offsetRect(layout.cardBounds, offset)
	accentBounds := offsetRect(layout.accentBounds, offset)
	typeBounds := offsetRect(layout.typeBounds, offset)
	deadlineBounds := offsetRect(layout.deadlineBounds, offset)
	countdownBounds := offsetRect(layout.countdownBounds, offset)
	tagBounds := offsetRect(layout.tagBounds, offset)
	titleBounds := offsetRect(layout.titleBounds, offset)
	detailsBounds := offsetRect(layout.detailsBounds, offset)
	checkBounds := offsetRect(layout.checkBounds, offset)
	postponeBounds := offsetRect(layout.postponeBounds, offset)

	accentColor, typeFg := todoTypeColors(layout.todo.Kind)
	cardFill := rgb(254, 255, 255)
	cardBorder := rgb(221, 227, 235)
	checkFill := rgb(250, 252, 255)
	checkBorder := rgb(148, 159, 171)
	checkMarkColor := rgb(255, 255, 255)
	deadlineFill := rgb(244, 247, 251)
	deadlineTextColor := rgb(98, 107, 116)
	tagFill := rgb(236, 242, 249)
	tagTextColor := rgb(72, 90, 112)
	postponeFill := rgb(246, 249, 253)
	postponeBorder := rgb(185, 198, 214)
	postponeTextColor := rgb(66, 93, 122)
	titleColor := rgb(21, 28, 35)
	detailsColor := rgb(92, 101, 111)
	urgencyTint := urgencyTintRatio(layout.todo.Deadline, p.urgencyTintDays)
	countdownFill, countdownTextColor := countdownPillColors(urgencyTint)
	if removing {
		checkMarkColor = accentColor
	}
	_ = fade

	p.fillRoundedRect(canvas, cardFill, cardBorder, cardBounds, cardRadius)
	p.fillRoundedRect(canvas, accentColor, accentColor, accentBounds, 3)

	p.drawPill(canvas, typeBounds, accentColor, typeFg, string(layout.todo.Kind))
	p.drawPill(canvas, deadlineBounds, deadlineFill, deadlineTextColor, "\u622a\u6b62 "+formatTodoDeadline(layout.todo.Deadline))
	p.drawPill(canvas, countdownBounds, countdownFill, countdownTextColor, deadlineCountdownLabel(layout.todo.Deadline))
	if tag := firstTag(layout.todo.Tags); tag != "" {
		p.drawPill(canvas, tagBounds, tagFill, tagTextColor, tag)
	}
	if showAction {
		label := layout.actionLabel
		if strings.TrimSpace(label) == "" {
			label = "\u63a8\u8fdf"
		}
		p.drawActionPill(canvas, postponeBounds, postponeFill, postponeBorder, postponeTextColor, label)
	}

	_ = canvas.DrawTextPixels(
		layout.todo.Title,
		p.titleFont,
		titleColor,
		titleBounds,
		walk.TextWordbreak|walk.TextEditControl|walk.TextEndEllipsis|walk.TextNoPrefix,
	)

	if strings.TrimSpace(layout.bodyText) != "" && detailsBounds.Height > 0 {
		_ = canvas.DrawTextPixels(
			layout.bodyText,
			p.bodyFont,
			detailsColor,
			detailsBounds,
			walk.TextWordbreak|walk.TextEditControl|walk.TextEndEllipsis|walk.TextNoPrefix,
		)
	}

	if showCheck {
		p.fillEllipse(canvas, checkFill, checkBorder, checkBounds)
		if removing {
			p.drawCheckMark(canvas, checkBounds, checkMarkColor)
		}
	}
}

func (p *CardPainter) drawPill(canvas *walk.Canvas, bounds walk.Rectangle, bg, fg walk.Color, text string) {
	p.fillRoundedRect(canvas, bg, bg, bounds, 12)
	_ = canvas.DrawTextPixels(text, p.metaFont, fg, bounds, walk.TextSingleLine|walk.TextCenter|walk.TextVCenter|walk.TextNoPrefix)
}

func (p *CardPainter) drawActionPill(canvas *walk.Canvas, bounds walk.Rectangle, bg, border, fg walk.Color, text string) {
	p.fillRoundedRect(canvas, bg, border, bounds, 13)
	_ = canvas.DrawTextPixels(text, p.metaFont, fg, bounds, walk.TextSingleLine|walk.TextCenter|walk.TextVCenter|walk.TextNoPrefix)
}

func (p *CardPainter) fillRoundedRect(canvas *walk.Canvas, fill, stroke walk.Color, bounds walk.Rectangle, radius int) {
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

func (p *CardPainter) fillEllipse(canvas *walk.Canvas, fill, stroke walk.Color, bounds walk.Rectangle) {
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

	_ = canvas.FillEllipsePixels(brush, bounds)
	_ = canvas.DrawEllipsePixels(pen, bounds)
}

func (p *CardPainter) drawCheckMark(canvas *walk.Canvas, bounds walk.Rectangle, color walk.Color) {
	brush, err := walk.NewSolidColorBrush(color)
	if err != nil {
		return
	}
	defer brush.Dispose()

	pen, err := walk.NewGeometricPen(walk.PenSolid|walk.PenJoinRound|walk.PenCapRound, 2, brush)
	if err != nil {
		return
	}
	defer pen.Dispose()

	points := []walk.Point{
		{X: bounds.X + 7, Y: bounds.Y + 16},
		{X: bounds.X + 12, Y: bounds.Y + 21},
		{X: bounds.X + 22, Y: bounds.Y + 10},
	}
	_ = canvas.DrawPolylinePixels(pen, points)
}

type TodoBoardWidget struct {
	*walk.CustomWidget
	painter       *CardPainter
	onComplete    func(string)
	onPostpone    func(string)
	onOpenDetails func(string)
	todos         []Todo
	layouts       []cardLayout
	layoutWidth   int
	totalHeight   int
	widthHint     int
	layoutDirty   bool
	removing      map[string]bool
	removalOffset map[string]int
	removalFade   map[string]float64
	actionLabel   string
	showCheck     bool
	showAction    bool
}

func NewTodoBoardWidget(parent walk.Container, painter *CardPainter, onComplete func(string), onPostpone func(string), onOpenDetails func(string), actionLabel string, showCheck bool, showAction bool) (*TodoBoardWidget, error) {
	board := &TodoBoardWidget{
		painter:       painter,
		onComplete:    onComplete,
		onPostpone:    onPostpone,
		onOpenDetails: onOpenDetails,
		widthHint:     620,
		layoutDirty:   true,
		removing:      map[string]bool{},
		removalOffset: map[string]int{},
		removalFade:   map[string]float64{},
		actionLabel:   actionLabel,
		showCheck:     showCheck,
		showAction:    showAction,
	}

	cw, err := walk.NewCustomWidgetPixels(parent, 0, func(canvas *walk.Canvas, updateBounds walk.Rectangle) error {
		return board.paint(canvas, updateBounds)
	})
	if err != nil {
		return nil, err
	}

	board.CustomWidget = cw
	if err := walk.InitWrapperWindow(board); err != nil {
		board.Dispose()
		return nil, err
	}

	board.SetBackground(walk.NullBrush())
	board.SetPaintMode(walk.PaintBuffered)
	board.SetInvalidatesOnResize(true)
	board.SetWidthHint(board.widthHint)

	board.MouseUp().Attach(func(x, y int, button walk.MouseButton) {
		switch button {
		case walk.LeftButton:
			if id, ok := board.hitPostpone(x, y); ok && board.onPostpone != nil {
				board.onPostpone(id)
				return
			}
			if id, ok := board.hitCheck(x, y); ok && board.onComplete != nil {
				board.onComplete(id)
			}
		case walk.RightButton:
			if id, ok := board.hitTodo(x, y); ok && board.onOpenDetails != nil {
				board.onOpenDetails(id)
			}
		}
	})

	return board, nil
}

func (b *TodoBoardWidget) CreateLayoutItem(ctx *walk.LayoutContext) walk.LayoutItem {
	return &todoBoardLayoutItem{board: b}
}

func (b *TodoBoardWidget) SetWidthHint(width int) {
	width = clampInt(width, minBoardWidth, 1200)
	if width == b.widthHint {
		return
	}

	b.widthHint = width
	_ = b.SetMinMaxSize(
		walk.Size{Width: b.widthHint, Height: emptyStateHeight},
		walk.Size{Width: b.widthHint, Height: 0},
	)
	b.invalidateLayout()
}

func (b *TodoBoardWidget) SetTodos(todos []Todo) {
	b.todos = append([]Todo(nil), todos...)
	b.removing = map[string]bool{}
	b.removalOffset = map[string]int{}
	b.removalFade = map[string]float64{}
	b.invalidateLayout()
}

func (b *TodoBoardWidget) PreferredHeight(width int) int {
	if width <= 0 {
		width = b.widthHint
	}
	_, totalHeight := b.ensureLayout(width)
	return totalHeight
}

func (b *TodoBoardWidget) BeginRemoval(id string) bool {
	for _, todo := range b.todos {
		if todo.ID != id {
			continue
		}
		if b.removing[id] {
			return false
		}
		b.removing[id] = true
		b.removalOffset[id] = 0
		b.removalFade[id] = 0
		_ = b.Invalidate()
		return true
	}

	return false
}

func (b *TodoBoardWidget) RemovalTargetOffset(id string) int {
	layouts, _ := b.ensureLayout(b.ClientBoundsPixels().Width)
	for _, layout := range layouts {
		if layout.todo.ID == id {
			target := int(float64(layout.cardBounds.Width) * removalHideRatio)
			return clampInt(target, 180, 960)
		}
	}

	return clampInt(int(float64(b.widthHint)*removalHideRatio), 180, 960)
}

func (b *TodoBoardWidget) SetRemovalOffset(id string, offset int) {
	if !b.removing[id] {
		return
	}

	b.removalOffset[id] = offset
	_ = b.Invalidate()
}

func (b *TodoBoardWidget) CancelRemoval(id string) {
	delete(b.removing, id)
	delete(b.removalOffset, id)
	delete(b.removalFade, id)
	_ = b.Invalidate()
}

func (b *TodoBoardWidget) SetRemovalFade(id string, fade float64) {
	if !b.removing[id] {
		return
	}

	if fade < 0 {
		fade = 0
	}
	if fade > 1 {
		fade = 1
	}
	b.removalFade[id] = fade
	_ = b.Invalidate()
}

func (b *TodoBoardWidget) paint(canvas *walk.Canvas, _ walk.Rectangle) error {
	background, err := walk.NewSolidColorBrush(boardBackgroundColor())
	if err == nil {
		defer background.Dispose()
		_ = canvas.FillRectanglePixels(background, b.ClientBoundsPixels())
	}

	width := b.ClientBoundsPixels().Width
	if width <= 0 {
		width = b.widthHint
	}
	layouts, _ := b.ensureLayout(width)
	if len(layouts) == 0 {
		return b.drawEmptyState(canvas)
	}

	for _, layout := range layouts {
		offset := b.removalOffset[layout.todo.ID]
		fade := b.removalFade[layout.todo.ID]
		b.painter.drawCard(canvas, layout, offset, fade, b.removing[layout.todo.ID], b.showCheck, b.showAction)
	}

	return nil
}

func (b *TodoBoardWidget) drawEmptyState(canvas *walk.Canvas) error {
	emptyRect := b.ClientBoundsPixels()
	emptyRect.X += 12
	emptyRect.Y += 18
	emptyRect.Width -= 24
	emptyRect.Height -= 36
	if emptyRect.Height < 72 {
		emptyRect.Height = 72
	}

	_ = canvas.DrawTextPixels(
		"\u5f53\u524d\u8fd8\u6ca1\u6709\u5f85\u529e\u3002\r\n\r\n\u70b9\u51fb\u4e0b\u65b9\u201c\u65b0\u589e\u4e8b\u9879\u201d\u5f00\u59cb\u8bb0\u5f55\uff0c\u5185\u5bb9\u8f83\u591a\u65f6\u4f1a\u81ea\u52a8\u51fa\u73b0\u6eda\u52a8\u6761\u3002",
		b.painter.bodyFont,
		rgb(100, 108, 117),
		emptyRect,
		walk.TextWordbreak|walk.TextVCenter|walk.TextCenter|walk.TextNoPrefix,
	)

	return nil
}

func (b *TodoBoardWidget) hitCheck(x, y int) (string, bool) {
	if !b.showCheck {
		return "", false
	}
	layouts, _ := b.ensureLayout(b.ClientBoundsPixels().Width)
	for _, layout := range layouts {
		checkRect := offsetRect(layout.checkBounds, b.removalOffset[layout.todo.ID])
		if pointInRect(x, y, checkRect) {
			return layout.todo.ID, true
		}
	}

	return "", false
}

func (b *TodoBoardWidget) hitPostpone(x, y int) (string, bool) {
	if !b.showAction {
		return "", false
	}
	layouts, _ := b.ensureLayout(b.ClientBoundsPixels().Width)
	for _, layout := range layouts {
		postponeRect := offsetRect(layout.postponeBounds, b.removalOffset[layout.todo.ID])
		if pointInRect(x, y, postponeRect) {
			return layout.todo.ID, true
		}
	}

	return "", false
}

func (b *TodoBoardWidget) hitTodo(x, y int) (string, bool) {
	layouts, _ := b.ensureLayout(b.ClientBoundsPixels().Width)
	for _, layout := range layouts {
		cardRect := offsetRect(layout.cardBounds, b.removalOffset[layout.todo.ID])
		if pointInRect(x, y, cardRect) {
			return layout.todo.ID, true
		}
	}

	return "", false
}

func (b *TodoBoardWidget) ensureLayout(width int) ([]cardLayout, int) {
	if width <= 0 {
		width = b.widthHint
	}
	if width < minBoardWidth {
		width = minBoardWidth
	}

	if !b.layoutDirty && b.layoutWidth == width {
		return b.layouts, b.totalHeight
	}

	layouts := make([]cardLayout, 0, len(b.todos))
	totalHeight := emptyStateHeight
	if len(b.todos) > 0 {
		y := boardPaddingY
		for _, todo := range b.todos {
			layout := b.painter.layoutTodo(todo, width, y, b.actionLabel)
			layouts = append(layouts, layout)
			y += layout.height + cardSpacing
		}
		totalHeight = y - cardSpacing + boardPaddingY
	}

	b.layouts = layouts
	b.layoutWidth = width
	b.totalHeight = totalHeight
	b.layoutDirty = false
	return b.layouts, b.totalHeight
}

func (b *TodoBoardWidget) invalidateLayout() {
	b.layoutDirty = true
	b.layoutWidth = 0
	b.layouts = nil
	b.totalHeight = 0
	b.RequestLayout()
	_ = b.Invalidate()
}

type todoBoardLayoutItem struct {
	walk.LayoutItemBase
	board *TodoBoardWidget
}

func (*todoBoardLayoutItem) LayoutFlags() walk.LayoutFlags {
	return walk.GrowableHorz | walk.GreedyHorz | walk.ShrinkableVert
}

func (li *todoBoardLayoutItem) IdealSize() walk.Size {
	width := li.board.widthHint
	if width < minBoardWidth {
		width = 620
	}

	return walk.Size{Width: width, Height: li.board.PreferredHeight(width)}
}

func (li *todoBoardLayoutItem) MinSize() walk.Size {
	width := li.board.widthHint
	if width < minBoardWidth {
		width = minBoardWidth
	}
	return walk.Size{Width: width, Height: li.board.PreferredHeight(width)}
}

func (*todoBoardLayoutItem) HasHeightForWidth() bool {
	return true
}

func (li *todoBoardLayoutItem) HeightForWidth(width int) int {
	return li.board.PreferredHeight(width)
}

func offsetRect(rect walk.Rectangle, offset int) walk.Rectangle {
	rect.X += offset
	return rect
}

func todoTypeColors(kind TodoType) (walk.Color, walk.Color) {
	switch kind {
	case TodoUrgentImportant:
		return rgb(207, 84, 68), rgb(255, 255, 255)
	case TodoUrgentNotImportant:
		return rgb(219, 149, 84), rgb(255, 255, 255)
	case TodoImportantNotUrgent:
		return rgb(40, 128, 148), rgb(255, 255, 255)
	default:
		return rgb(130, 138, 147), rgb(255, 255, 255)
	}
}

func countdownPillColors(urgencyTint float64) (walk.Color, walk.Color) {
	baseFill := rgb(242, 246, 250)
	baseText := rgb(101, 109, 118)
	targetFill := rgb(252, 231, 228)
	targetText := rgb(174, 58, 47)
	return blendColor(baseFill, targetFill, urgencyTint), blendColor(baseText, targetText, urgencyTint)
}

func urgencyTintRatio(value string, startDays int) float64 {
	if startDays <= 0 {
		return 0
	}

	deadline := parseStoredTime(value, time.Time{})
	if deadline.IsZero() {
		return 0
	}

	diff := deadline.Sub(time.Now())
	if diff <= 0 {
		return 1
	}

	window := time.Duration(startDays) * 24 * time.Hour
	if diff >= window {
		return 0
	}

	ratio := 1 - diff.Seconds()/window.Seconds()
	if ratio <= 0 {
		return 0
	}
	if ratio >= 1 {
		return 1
	}
	return math.Pow(ratio, 0.92)
}

func boardBackgroundColor() walk.Color {
	return rgb(246, 248, 252)
}

func blendColor(left, right walk.Color, ratio float64) walk.Color {
	if ratio <= 0 {
		return left
	}
	if ratio >= 1 {
		return right
	}

	lr, lg, lb := colorBytes(left)
	rr, rg, rb := colorBytes(right)
	return rgb(
		byte(float64(lr)+float64(rr-lr)*ratio),
		byte(float64(lg)+float64(rg-lg)*ratio),
		byte(float64(lb)+float64(rb-lb)*ratio),
	)
}

func colorBytes(color walk.Color) (byte, byte, byte) {
	value := uint32(color)
	return byte(value), byte(value >> 8), byte(value >> 16)
}

func pointInRect(x, y int, rect walk.Rectangle) bool {
	return x >= rect.X && x <= rect.X+rect.Width && y >= rect.Y && y <= rect.Y+rect.Height
}

func cardBodyText(todo Todo) string {
	if progress := strings.TrimSpace(todo.Progress); progress != "" {
		return "\u8fdb\u5c55\uff1a" + progress
	}
	return strings.TrimSpace(todo.Details)
}

func formatTodoDeadline(value string) string {
	t := parseStoredTime(value, time.Time{})
	if t.IsZero() {
		return "\u672a\u8bbe\u7f6e"
	}

	if utf8.RuneCountInString(value) == 0 {
		return "\u672a\u8bbe\u7f6e"
	}
	return t.Local().Format("2006-01-02 15:04")
}

func firstTag(tags []string) string {
	if len(tags) == 0 {
		return ""
	}
	for _, tag := range tags {
		if strings.TrimSpace(tag) != "" {
			return strings.TrimSpace(tag)
		}
	}
	return ""
}
