package main

import (
	"math"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
	"unsafe"

	"github.com/lxn/walk"
	"github.com/lxn/win"
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
	urgencyTintPercent int
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
		urgencyTintPercent: 35,
	}, nil
}

func (p *CardPainter) SetUrgencyTintDays(days int) {
	if days <= 0 {
		days = 35
	}
	if days > 100 {
		days = 100
	}
	p.urgencyTintPercent = days
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
	scale := cardScaleFactor(width)
	s := func(value int) int {
		return scaleMetric(value, scale)
	}

	availableWidth := width - s(boardPaddingX)*2
	cardWidth := int(math.Round(float64(width) * 0.90))
	if cardWidth > availableWidth {
		cardWidth = availableWidth
	}
	if cardWidth < 320 {
		cardWidth = 320
	}

	cardX := (width - cardWidth) / 2
	if cardX < s(boardPaddingX) {
		cardX = s(boardPaddingX)
	}
	textWidth := cardWidth - s(cardHorizontalInset)*2 - s(checkAreaWidth)
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

	contentX := cardX + s(cardHorizontalInset)
	contentY := y + s(cardTopInset) + s(10)

	metaHeight := s(pillHeight)
	gap := s(pillGap)
	rowGap := s(metaRowGap)
	countdownX := contentX + typeWidth + gap + deadlineWidth + gap
	countdownY := contentY
	tagX := countdownX + countdownWidth + gap
	tagY := contentY
	if typeWidth+gap+deadlineWidth+gap+countdownWidth+gap+tagWidth > textWidth {
		metaHeight = s(pillHeight)*2 + rowGap
		countdownX = contentX
		countdownY = contentY + s(pillHeight) + rowGap
		tagX = countdownX + countdownWidth + gap
		tagY = countdownY
	}

	titleHeight := p.measureTextHeight(todo.Title, p.titleFont, textWidth, 56)
	if titleHeight < 26 {
		titleHeight = 26
	}

	bodyText := cardBodyText(todo)
	detailsHeight := p.measureTextHeight(bodyText, p.bodyFont, textWidth, maxBodyHeight)

	height := s(cardTopInset) + s(8) + metaHeight + s(metaToTitleGap) + titleHeight + s(cardBottomInset)
	if detailsHeight > 0 {
		height += s(titleToBodyGap) + detailsHeight
	}
	if height < s(cardMinHeight) {
		height = s(cardMinHeight)
	}

	cardBounds := walk.Rectangle{X: cardX, Y: y, Width: cardWidth, Height: height}
	accentBounds := walk.Rectangle{X: cardBounds.X + s(16), Y: cardBounds.Y + s(12), Width: cardBounds.Width - s(32), Height: s(5)}
	contentX = cardBounds.X + s(cardHorizontalInset)
	contentY = cardBounds.Y + s(cardTopInset) + s(10)

	typeBounds := walk.Rectangle{X: contentX, Y: contentY, Width: typeWidth, Height: s(pillHeight)}
	deadlineBounds := walk.Rectangle{X: typeBounds.X + typeBounds.Width + gap, Y: contentY, Width: deadlineWidth, Height: s(pillHeight)}
	countdownBounds := walk.Rectangle{X: countdownX, Y: countdownY, Width: countdownWidth, Height: s(pillHeight)}
	tagBounds := walk.Rectangle{X: tagX, Y: tagY, Width: tagWidth, Height: s(pillHeight)}
	titleY := contentY + metaHeight + s(metaToTitleGap)
	titleBounds := walk.Rectangle{X: contentX, Y: titleY, Width: textWidth, Height: titleHeight}
	detailsBounds := walk.Rectangle{X: contentX, Y: titleY + titleHeight + s(titleToBodyGap), Width: textWidth, Height: detailsHeight}
	actionColumnWidth := maxInt(s(checkSize), s(postponeButtonWidth))
	actionColumnX := cardBounds.X + cardBounds.Width - s(cardHorizontalInset) - actionColumnWidth
	checkBounds := walk.Rectangle{
		X:      actionColumnX + (actionColumnWidth-s(checkSize))/2,
		Y:      cardBounds.Y + (cardBounds.Height-s(checkSize))/2,
		Width:  s(checkSize),
		Height: s(checkSize),
	}
	postponeBounds := walk.Rectangle{
		X:      actionColumnX + (actionColumnWidth-s(postponeButtonWidth))/2,
		Y:      cardBounds.Y + cardBounds.Height - s(cardBottomInset) - s(postponeButtonHeight) + s(2),
		Width:  s(postponeButtonWidth),
		Height: s(postponeButtonHeight),
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

func (p *CardPainter) drawCard(canvas *walk.Canvas, layout cardLayout, showCheck bool, showAction bool, showCountdown bool) {
	cardBounds := layout.cardBounds
	accentBounds := layout.accentBounds
	typeBounds := layout.typeBounds
	deadlineBounds := layout.deadlineBounds
	countdownBounds := layout.countdownBounds
	tagBounds := layout.tagBounds
	titleBounds := layout.titleBounds
	detailsBounds := layout.detailsBounds
	checkBounds := layout.checkBounds
	postponeBounds := layout.postponeBounds

	accentColor, typeFg := todoTypeColors(layout.todo.Kind)
	cardFill := rgb(254, 255, 255)
	cardBorder := rgb(221, 227, 235)
	checkFill := rgb(250, 252, 255)
	checkBorder := rgb(148, 159, 171)
	deadlineFill := rgb(244, 247, 251)
	deadlineTextColor := rgb(98, 107, 116)
	tagFill := rgb(236, 242, 249)
	tagTextColor := rgb(72, 90, 112)
	postponeFill := rgb(246, 249, 253)
	postponeBorder := rgb(185, 198, 214)
	postponeTextColor := rgb(66, 93, 122)
	titleColor := rgb(21, 28, 35)
	detailsColor := rgb(92, 101, 111)
	urgencyTint := urgencyTintRatio(layout.todo.CreatedAt, layout.todo.Deadline, p.urgencyTintPercent)
	countdownFill, countdownTextColor := countdownPillColors(urgencyTint)
	scale := cardScaleFactor(layout.cardBounds.Width)
	p.fillRoundedRect(canvas, cardFill, cardBorder, cardBounds, scaleMetric(cardRadius, scale))
	p.fillRoundedRect(canvas, accentColor, accentColor, accentBounds, scaleMetric(3, scale))

	p.drawPill(canvas, typeBounds, accentColor, typeFg, string(layout.todo.Kind))
	p.drawPill(canvas, deadlineBounds, deadlineFill, deadlineTextColor, "\u622a\u6b62 "+formatTodoDeadline(layout.todo.Deadline))
	if showCountdown {
		p.drawPill(canvas, countdownBounds, countdownFill, countdownTextColor, deadlineCountdownLabel(layout.todo.Deadline))
	}
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
	}
}

func (p *CardPainter) drawPill(canvas *walk.Canvas, bounds walk.Rectangle, bg, fg walk.Color, text string) {
	p.fillRoundedRect(canvas, bg, bg, bounds, maxInt(8, bounds.Height/2))
	_ = canvas.DrawTextPixels(text, p.metaFont, fg, bounds, walk.TextSingleLine|walk.TextCenter|walk.TextVCenter|walk.TextNoPrefix)
}

func (p *CardPainter) drawActionPill(canvas *walk.Canvas, bounds walk.Rectangle, bg, border, fg walk.Color, text string) {
	p.fillRoundedRect(canvas, bg, border, bounds, maxInt(8, bounds.Height/2))
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
	actionLabel   string
	showCheck     bool
	showAction    bool
	showCountdown bool
	skinBitmap    *walk.Bitmap
	skinOpacity   byte
	cacheBitmap   *walk.Bitmap
	cacheWidth    int
	cacheHeight   int
	cacheDirty    bool
	scrollY       int
}

func NewTodoBoardWidget(parent walk.Container, painter *CardPainter, onComplete func(string), onPostpone func(string), onOpenDetails func(string), actionLabel string, showCheck bool, showAction bool, showCountdown bool) (*TodoBoardWidget, error) {
	board := &TodoBoardWidget{
		painter:       painter,
		onComplete:    onComplete,
		onPostpone:    onPostpone,
		onOpenDetails: onOpenDetails,
		widthHint:     620,
		layoutDirty:   true,
		actionLabel:   actionLabel,
		showCheck:     showCheck,
		showAction:    showAction,
		showCountdown: showCountdown,
		cacheDirty:    true,
	}

	cw, err := walk.NewCustomWidgetPixels(parent, uint(win.WS_VSCROLL), func(canvas *walk.Canvas, updateBounds walk.Rectangle) error {
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
		y += board.scrollY
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
	b.invalidateLayout()
}

func (b *TodoBoardWidget) PreferredHeight(width int) int {
	return emptyStateHeight
}

func (b *TodoBoardWidget) paint(canvas *walk.Canvas, updateBounds walk.Rectangle) error {
	if updateBounds.Width <= 0 || updateBounds.Height <= 0 {
		updateBounds = b.ClientBoundsPixels()
	}
	width := b.ClientBoundsPixels().Width
	if width <= 0 {
		width = b.widthHint
	}
	layouts, totalHeight := b.ensureLayout(width)
	b.updateScrollBar(totalHeight)
	if err := b.ensureCache(width, totalHeight, layouts); err != nil {
		return err
	}

	if b.cacheBitmap == nil {
		return nil
	}

	src := walk.Rectangle{
		X:      updateBounds.X,
		Y:      updateBounds.Y + b.scrollY,
		Width:  updateBounds.Width,
		Height: updateBounds.Height,
	}
	if src.Y+src.Height > b.cacheHeight {
		src.Height = maxInt(0, b.cacheHeight-src.Y)
	}
	if src.Height > 0 {
		if err := canvas.DrawBitmapPartWithOpacityPixels(b.cacheBitmap, updateBounds, src, 0xff); err != nil {
			return err
		}
	}
	if src.Height < updateBounds.Height {
		fillRect := walk.Rectangle{X: updateBounds.X, Y: updateBounds.Y + src.Height, Width: updateBounds.Width, Height: updateBounds.Height - src.Height}
		if fillRect.Height > 0 {
			background, err := walk.NewSolidColorBrush(boardBackgroundColor())
			if err == nil {
				defer background.Dispose()
				_ = canvas.FillRectanglePixels(background, fillRect)
			}
		}
	}
	return nil
}

func (b *TodoBoardWidget) SetSkin(bitmap *walk.Bitmap, opacityPercent int) {
	b.skinBitmap = bitmap
	if opacityPercent < 0 {
		opacityPercent = 0
	}
	if opacityPercent > 100 {
		opacityPercent = 100
	}
	b.skinOpacity = byte(math.Round(float64(opacityPercent) * 255 / 100))
	b.cacheDirty = true
	_ = b.Invalidate()
}

func (b *TodoBoardWidget) drawEmptyState(canvas *walk.Canvas, updateBounds walk.Rectangle) error {
	emptyRect := b.ClientBoundsPixels()
	emptyRect.X += 12
	emptyRect.Y += 18
	emptyRect.Width -= 24
	emptyRect.Height -= 36
	if emptyRect.Height < 72 {
		emptyRect.Height = 72
	}
	if clipped, ok := intersectRect(emptyRect, updateBounds); ok {
		emptyRect = clipped
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
		if pointInRect(x, y, layout.checkBounds) {
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
		if pointInRect(x, y, layout.postponeBounds) {
			return layout.todo.ID, true
		}
	}

	return "", false
}

func (b *TodoBoardWidget) hitTodo(x, y int) (string, bool) {
	layouts, _ := b.ensureLayout(b.ClientBoundsPixels().Width)
	for _, layout := range layouts {
		if pointInRect(x, y, layout.cardBounds) {
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
	b.cacheDirty = true
	b.RequestLayout()
	b.updateScrollBar(emptyStateHeight)
	_ = b.Invalidate()
}

func (b *TodoBoardWidget) ensureCache(width, totalHeight int, layouts []cardLayout) error {
	if width <= 0 {
		width = b.widthHint
	}
	if totalHeight <= 0 {
		totalHeight = emptyStateHeight
	}
	if !b.cacheDirty && b.cacheBitmap != nil && b.cacheWidth == width && b.cacheHeight == totalHeight {
		return nil
	}

	if b.cacheBitmap != nil {
		b.cacheBitmap.Dispose()
		b.cacheBitmap = nil
	}

	bmp, err := walk.NewBitmapForDPI(walk.Size{Width: width, Height: totalHeight}, b.DPI())
	if err != nil {
		return err
	}

	cacheCanvas, err := walk.NewCanvasFromImage(bmp)
	if err != nil {
		bmp.Dispose()
		return err
	}
	defer cacheCanvas.Dispose()

	fullBounds := walk.Rectangle{Width: width, Height: totalHeight}
	background, err := walk.NewSolidColorBrush(boardBackgroundColor())
	if err == nil {
		defer background.Dispose()
		_ = cacheCanvas.FillRectanglePixels(background, fullBounds)
	}

	if b.skinBitmap != nil && b.skinOpacity > 0 {
		size := b.skinBitmap.Size()
		if size.Width > 0 && size.Height > 0 {
			scale := float64(totalHeight) / float64(size.Height)
			drawWidth := int(math.Round(float64(size.Width) * scale))
			drawBounds := walk.Rectangle{
				X:      (width - drawWidth) / 2,
				Y:      0,
				Width:  drawWidth,
				Height: totalHeight,
			}
			_ = cacheCanvas.DrawBitmapWithOpacityPixels(b.skinBitmap, drawBounds, b.skinOpacity)
		}
	}

	if len(layouts) == 0 {
		if err := b.drawEmptyState(cacheCanvas, fullBounds); err != nil {
			bmp.Dispose()
			return err
		}
	} else {
		for _, layout := range layouts {
			b.painter.drawCard(cacheCanvas, layout, b.showCheck, b.showAction, b.showCountdown)
		}
	}

	b.cacheBitmap = bmp
	b.cacheWidth = width
	b.cacheHeight = totalHeight
	b.cacheDirty = false
	return nil
}

type todoBoardLayoutItem struct {
	walk.LayoutItemBase
	board *TodoBoardWidget
}

func (*todoBoardLayoutItem) LayoutFlags() walk.LayoutFlags {
	return walk.GrowableHorz | walk.GreedyHorz | walk.GrowableVert | walk.GreedyVert
}

func (li *todoBoardLayoutItem) IdealSize() walk.Size {
	width := li.board.widthHint
	if width < minBoardWidth {
		width = 620
	}

	return walk.Size{Width: width, Height: 360}
}

func (li *todoBoardLayoutItem) MinSize() walk.Size {
	width := li.board.widthHint
	if width < minBoardWidth {
		width = minBoardWidth
	}
	return walk.Size{Width: width, Height: emptyStateHeight}
}

func (*todoBoardLayoutItem) HasHeightForWidth() bool {
	return true
}

func (li *todoBoardLayoutItem) HeightForWidth(width int) int {
	return emptyStateHeight
}

func (b *TodoBoardWidget) WndProc(hwnd win.HWND, msg uint32, wParam, lParam uintptr) uintptr {
	switch msg {
	case win.WM_MOUSEWHEEL:
		delta := int16(win.HIWORD(uint32(wParam)))
		lines := 3
		step := 36 * lines
		if delta < 0 {
			b.setScrollY(b.scrollY + step)
		} else {
			b.setScrollY(b.scrollY - step)
		}
		return 0

	case win.WM_VSCROLL:
		cmd := win.LOWORD(uint32(wParam))
		switch cmd {
		case win.SB_LINEUP:
			b.setScrollY(b.scrollY - 36)
		case win.SB_LINEDOWN:
			b.setScrollY(b.scrollY + 36)
		case win.SB_PAGEUP:
			b.setScrollY(b.scrollY - b.ClientBoundsPixels().Height)
		case win.SB_PAGEDOWN:
			b.setScrollY(b.scrollY + b.ClientBoundsPixels().Height)
		case win.SB_THUMBPOSITION, win.SB_THUMBTRACK:
			var si win.SCROLLINFO
			si.CbSize = uint32(unsafe.Sizeof(si))
			si.FMask = win.SIF_TRACKPOS
			if win.GetScrollInfo(hwnd, win.SB_VERT, &si) {
				b.setScrollY(int(si.NTrackPos))
			}
		}
		return 0

	case win.WM_WINDOWPOSCHANGED:
		result := b.CustomWidget.WndProc(hwnd, msg, wParam, lParam)
		width := b.ClientBoundsPixels().Width
		if width <= 0 {
			width = b.widthHint
		}
		_, totalHeight := b.ensureLayout(width)
		b.updateScrollBar(totalHeight)
		return result
	}

	return b.CustomWidget.WndProc(hwnd, msg, wParam, lParam)
}

func (b *TodoBoardWidget) setScrollY(value int) {
	maxScroll := b.maxScrollY()
	value = clampInt(value, 0, maxScroll)
	if value == b.scrollY {
		return
	}
	b.scrollY = value
	b.updateScrollBar(b.totalHeight)
	_ = b.Invalidate()
}

func (b *TodoBoardWidget) maxScrollY() int {
	clientHeight := b.ClientBoundsPixels().Height
	if clientHeight <= 0 {
		clientHeight = emptyStateHeight
	}
	return maxInt(0, b.totalHeight-clientHeight)
}

func (b *TodoBoardWidget) updateScrollBar(totalHeight int) {
	if b.Handle() == 0 {
		return
	}
	clientHeight := b.ClientBoundsPixels().Height
	if clientHeight <= 0 {
		clientHeight = emptyStateHeight
	}
	maxScroll := maxInt(0, totalHeight-clientHeight)
	if b.scrollY > maxScroll {
		b.scrollY = maxScroll
	}
	var si win.SCROLLINFO
	si.CbSize = uint32(unsafe.Sizeof(si))
	si.FMask = win.SIF_RANGE | win.SIF_PAGE | win.SIF_POS
	si.NMin = 0
	si.NMax = int32(maxInt(totalHeight-1, 0))
	si.NPage = uint32(clientHeight)
	si.NPos = int32(b.scrollY)
	win.SetScrollInfo(b.Handle(), win.SB_VERT, &si, true)
}

func intersectRect(a, b walk.Rectangle) (walk.Rectangle, bool) {
	left := maxInt(a.X, b.X)
	top := maxInt(a.Y, b.Y)
	right := minInt(a.X+a.Width, b.X+b.Width)
	bottom := minInt(a.Y+a.Height, b.Y+b.Height)
	if right <= left || bottom <= top {
		return walk.Rectangle{}, false
	}
	return walk.Rectangle{
		X:      left,
		Y:      top,
		Width:  right - left,
		Height: bottom - top,
	}, true
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
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
	baseFill := rgb(244, 247, 251)
	baseText := rgb(98, 107, 116)
	targetFill := rgb(207, 84, 68)
	targetText := rgb(255, 255, 255)
	return blendColor(baseFill, targetFill, urgencyTint), blendColor(baseText, targetText, urgencyTint)
}

func urgencyTintRatio(createdValue, deadlineValue string, startPercent int) float64 {
	if startPercent <= 0 {
		return 0
	}

	deadline := parseStoredTime(deadlineValue, time.Time{})
	if deadline.IsZero() {
		return 0
	}
	createdAt := parseStoredTime(createdValue, time.Time{})
	if createdAt.IsZero() {
		return 0
	}

	now := time.Now()
	remaining := deadline.Sub(now)
	if remaining <= 0 {
		return 1
	}
	total := deadline.Sub(createdAt)
	if total <= 0 {
		return 0
	}
	window := time.Duration(float64(total) * (float64(startPercent) / 100.0))
	if window <= 0 {
		return 0
	}
	if remaining >= window {
		return 0
	}

	ratio := 1 - remaining.Seconds()/window.Seconds()
	if ratio <= 0 {
		return 0
	}
	if ratio >= 1 {
		return 1
	}
	return ratio
}

func boardBackgroundColor() walk.Color {
	return rgb(246, 248, 252)
}

func cardScaleFactor(width int) float64 {
	scale := float64(width) / 620.0
	if scale < 0.85 {
		return 0.85
	}
	if scale > 1.25 {
		return 1.25
	}
	return scale
}

func scaleMetric(value int, scale float64) int {
	scaled := int(math.Round(float64(value) * scale))
	if scaled < 1 {
		return 1
	}
	return scaled
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
		byte(float64(lr)+float64(int(rr)-int(lr))*ratio),
		byte(float64(lg)+float64(int(rg)-int(lg))*ratio),
		byte(float64(lb)+float64(int(rb)-int(lb))*ratio),
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
	if progress := latestProgressEntry(todo.Progress); progress != "" {
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

func latestProgressEntry(value string) string {
	value = strings.TrimSpace(strings.ReplaceAll(value, "\r\n", "\n"))
	if value == "" {
		return ""
	}
	lines := strings.Split(value, "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		if line != "" {
			return line
		}
	}
	return ""
}
