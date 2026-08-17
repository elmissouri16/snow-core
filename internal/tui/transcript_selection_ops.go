package tui

import (
	"os"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	osc52 "github.com/aymanbagabas/go-osc52/v2"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	xansi "github.com/charmbracelet/x/ansi"
)

func (m *Model) clearTranscriptSelection() {
	nextID := m.transcriptSelection.autoScrollID + 1
	m.transcriptSelection = transcriptSelectionState{autoScrollID: nextID}
	m.transcriptSelectionMenu = transcriptSelectionContextMenu{}
	m.transcriptSelectionView = ""
	m.transcriptSelectionViewValid = false
	m.transcriptSelectionRendered = ""
	m.transcriptSelectionRenderedValid = false
}

func splitTranscriptSelectionLines(content string) []string {
	if content == "" {
		return nil
	}
	return strings.Split(content, "\n")
}

func (m *Model) transcriptSelectionSourceLines() []string {
	if len(m.transcriptSelectionLines) == 0 && m.transcriptContent != "" {
		m.transcriptSelectionLines = splitTranscriptSelectionLines(m.transcriptContent)
	}
	return m.transcriptSelectionLines
}

func (m *Model) transcriptSelectionTop() int {
	// The full-screen frame always places the transcript after the one-row
	// header and separator. Inline mode leaves history to terminal selection.
	return 2
}

func (m *Model) transcriptSelectionPointAt(x, y int, clampToViewport bool) (transcriptSelectionPoint, bool) {
	if m.inlineTranscript || m.transcript.Height <= 0 || m.transcript.Width <= 0 {
		return transcriptSelectionPoint{}, false
	}
	top := m.transcriptSelectionTop()
	bottom := top + m.transcript.Height - 1
	if clampToViewport {
		x = min(max(0, x), m.transcript.Width-1)
		y = min(max(top, y), bottom)
	} else if x < 0 || x >= m.transcript.Width || y < top || y > bottom {
		return transcriptSelectionPoint{}, false
	}
	lines := m.transcriptSelectionSourceLines()
	if len(lines) == 0 {
		return transcriptSelectionPoint{}, false
	}
	row := max(0, m.transcript.YOffset+y-top)
	if !clampToViewport && row >= len(lines) {
		// The viewport pads short content to its configured height. A click in
		// those blank rows must not select the final actual transcript line.
		return transcriptSelectionPoint{}, false
	}
	row = min(row, len(lines)-1)
	return transcriptSelectionPoint{row: row, col: x}, true
}

func (m *Model) applyTranscriptSelectionMouse(msg tea.MouseMsg) (bool, tea.Cmd) {
	if m.app == nil || !m.app.Cfg.TUI.Mouse || m.inlineTranscript {
		return false, nil
	}
	event := tea.MouseEvent(msg)
	if m.transcriptSelectionMenu.open {
		if handled, cmd := m.applyTranscriptSelectionContextMenuMouse(event); handled {
			return true, cmd
		}
	}
	if event.Action == tea.MouseActionPress && event.Button == tea.MouseButtonRight {
		text := m.selectedTranscriptText()
		if text == "" {
			m.lastStatus = "drag transcript text to select and copy"
			return true, nil
		}
		m.openTranscriptSelectionContextMenu(event.X, event.Y, text)
		return true, nil
	}
	if event.Action == tea.MouseActionRelease {
		if !m.transcriptSelection.pressActive {
			return false, nil
		}
		m.transcriptSelection.pressActive = false
		m.stopTranscriptSelectionAutoScroll()
		if point, ok := m.transcriptSelectionPointAt(event.X, event.Y, true); ok {
			m.updateTranscriptSelectionFocus(point)
		}
		text := m.selectedTranscriptText()
		// Publish any stream updates that were frozen against the immutable drag
		// snapshot only after extracting the selected text.
		m.catchUpTranscriptAfterSelection()
		if text == "" {
			return true, nil
		}
		return true, m.copyTranscriptSelectionCmd(text)
	}

	if event.Action == tea.MouseActionMotion {
		if !m.transcriptSelection.pressActive || m.transcriptSelection.anchor == nil {
			return false, nil
		}
		point, ok := m.transcriptSelectionPointAt(event.X, event.Y, true)
		if !ok {
			return true, nil
		}
		// Bubble Tea already coalesces physical terminal writes. Update selection
		// immediately so a flood of cell-motion messages cannot queue ahead of a
		// second selection timer and leave the highlight trailing the pointer.
		focus := m.transcriptSelection.focus
		if focus != nil && focus.row == point.row && focus.col == point.col {
			return true, m.updateTranscriptSelectionAutoScroll(event.X, event.Y)
		}
		m.transcriptSelection.dragged = true
		m.transcriptSelection.lastClick = nil
		m.updateTranscriptSelectionFocus(point)
		return true, m.updateTranscriptSelectionAutoScroll(event.X, event.Y)
	}

	if event.Action != tea.MouseActionPress || event.Button != tea.MouseButtonLeft {
		return false, nil
	}
	point, ok := m.transcriptSelectionPointAt(event.X, event.Y, false)
	if !ok {
		m.clearTranscriptSelection()
		return false, nil
	}
	m.stopTranscriptSelectionAutoScroll()
	word, hasWord := m.transcriptWordRange(point)
	clickCount := m.transcriptSelectionClickCount(point, word, hasWord)
	var selected transcriptSelectionRange
	switch clickCount {
	case 2:
		selected = word
		m.transcriptSelection.granularity = transcriptSelectionWord
	case 3:
		selected = m.transcriptLineRange(point)
		m.transcriptSelection.granularity = transcriptSelectionLine
	default:
		selected = transcriptSelectionRange{start: point, end: point}
		m.transcriptSelection.granularity = transcriptSelectionCharacter
	}
	m.transcriptSelection.anchor = cloneTranscriptSelectionPoint(selected.start)
	m.transcriptSelection.focus = cloneTranscriptSelectionPoint(selected.end)
	m.transcriptSelection.initial = nil
	if clickCount == 2 || clickCount == 3 {
		initial := selected
		m.transcriptSelection.initial = &initial
	}
	m.transcriptSelection.pressActive = true
	m.transcriptSelection.dragged = false
	m.cacheTranscriptSelectionView()
	return true, nil
}

func (m *Model) copyTranscriptSelectionCmd(text string) tea.Cmd {
	copyText := m.copySelectionToClipboard
	return func() tea.Msg {
		message := transcriptSelectionCopiedMsg{characters: utf8.RuneCountInString(text)}
		if copyText != nil {
			if err := copyText(text); err == nil {
				return message
			}
		}
		// OSC 52 remains a portable fallback when a host clipboard utility is
		// unavailable (for example, a minimal Linux environment).
		message.sequence = transcriptSelectionClipboardSequence(text)
		return message
	}
}

func transcriptSelectionContextMenuView() string {
	item := lipgloss.NewStyle().
		Foreground(colorAccent).
		Bold(true).
		Padding(0, 1).
		Render("› Copy selection")
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(colorMuted).
		Render(item)
}

func transcriptSelectionBlockWidth(block string) int {
	width := 0
	for _, line := range strings.Split(block, "\n") {
		width = max(width, xansi.StringWidth(line))
	}
	return width
}

func (m *Model) openTranscriptSelectionContextMenu(x, y int, text string) {
	view := transcriptSelectionContextMenuView()
	frameWidth := m.managedFrameWidth()
	width := min(transcriptSelectionBlockWidth(view), frameWidth)
	height := lipgloss.Height(view)
	x = min(max(0, x), max(0, frameWidth-width))
	y = min(max(0, y+1), max(0, m.height-height))
	m.transcriptSelectionMenu = transcriptSelectionContextMenu{
		open: true, x: x, y: y, width: width, height: height, selectedText: text,
	}
}

func (m *Model) closeTranscriptSelectionContextMenu() {
	m.transcriptSelectionMenu = transcriptSelectionContextMenu{}
}

func (m *Model) applyTranscriptSelectionContextMenuMouse(event tea.MouseEvent) (bool, tea.Cmd) {
	menu := m.transcriptSelectionMenu
	if !menu.open {
		return false, nil
	}
	if event.Button == tea.MouseButtonWheelUp || event.Button == tea.MouseButtonWheelDown ||
		event.Button == tea.MouseButtonWheelLeft || event.Button == tea.MouseButtonWheelRight {
		m.closeTranscriptSelectionContextMenu()
		return false, nil
	}
	if event.Action == tea.MouseActionMotion || event.Action == tea.MouseActionRelease {
		return true, nil
	}
	if event.Action != tea.MouseActionPress {
		return true, nil
	}
	inside := event.X >= menu.x && event.X < menu.x+menu.width &&
		event.Y >= menu.y && event.Y < menu.y+menu.height
	if event.Button == tea.MouseButtonLeft && inside {
		m.closeTranscriptSelectionContextMenu()
		return true, m.copyTranscriptSelectionCmd(menu.selectedText)
	}
	if event.Button == tea.MouseButtonRight {
		m.openTranscriptSelectionContextMenu(event.X, event.Y, menu.selectedText)
		return true, nil
	}
	m.closeTranscriptSelectionContextMenu()
	return true, nil
}

func (m *Model) applyTranscriptSelectionContextMenuKey(msg tea.KeyMsg) (bool, tea.Cmd) {
	if !m.transcriptSelectionMenu.open {
		return false, nil
	}
	switch msg.Type {
	case tea.KeyEnter:
		text := m.transcriptSelectionMenu.selectedText
		m.closeTranscriptSelectionContextMenu()
		return true, m.copyTranscriptSelectionCmd(text)
	case tea.KeyEsc:
		m.closeTranscriptSelectionContextMenu()
		return true, nil
	}
	if msg.Type == tea.KeyRunes && strings.EqualFold(string(msg.Runes), "c") {
		text := m.transcriptSelectionMenu.selectedText
		m.closeTranscriptSelectionContextMenu()
		return true, m.copyTranscriptSelectionCmd(text)
	}
	m.closeTranscriptSelectionContextMenu()
	return false, nil
}

func overlayTranscriptSelectionContextMenu(frame string, menu transcriptSelectionContextMenu) string {
	if !menu.open || menu.width <= 0 || frame == "" {
		return frame
	}
	popup := transcriptSelectionContextMenuView()
	baseLines := strings.Split(frame, "\n")
	popupLines := strings.Split(popup, "\n")
	for index, popupLine := range popupLines {
		row := menu.y + index
		if row < 0 || row >= len(baseLines) {
			continue
		}
		line := baseLines[row]
		lineWidth := xansi.StringWidth(line)
		needed := menu.x + menu.width
		if lineWidth < needed {
			line += strings.Repeat(" ", needed-lineWidth)
			lineWidth = needed
		}
		popupWidth := xansi.StringWidth(popupLine)
		if popupWidth < menu.width {
			popupLine += strings.Repeat(" ", menu.width-popupWidth)
		} else if popupWidth > menu.width {
			popupLine = xansi.Cut(popupLine, 0, menu.width)
		}
		before := xansi.Cut(line, 0, menu.x)
		after := xansi.Cut(line, min(lineWidth, menu.x+menu.width), lineWidth)
		// The underlying transcript may be in reverse-video selection mode.
		// Reset around each popup row so that style cannot bleed across the frame;
		// xansi.Cut restores the source style at the start of after.
		baseLines[row] = before + "\x1b[0m" + popupLine + "\x1b[0m" + after
	}
	return strings.Join(baseLines, "\n")
}

func (m *Model) catchUpTranscriptAfterSelection() {
	if !m.transcriptDirty {
		return
	}
	wasAtBottom := m.transcript.AtBottom()
	m.flushTranscriptImmediately()
	if wasAtBottom {
		m.transcript.GotoBottom()
	}
}

func transcriptSelectionClipboardSequence(text string) string {
	sequence := osc52.New(text)
	if strings.TrimSpace(strings.ToLower(os.Getenv("TMUX"))) != "" {
		sequence = sequence.Tmux()
	} else if strings.HasPrefix(strings.ToLower(os.Getenv("TERM")), "screen") {
		sequence = sequence.Screen()
	}
	return sequence.String()
}

func cloneTranscriptSelectionPoint(point transcriptSelectionPoint) *transcriptSelectionPoint {
	copy := point
	return &copy
}

func (m *Model) transcriptSelectionClickCount(point transcriptSelectionPoint, word transcriptSelectionRange, hasWord bool) int {
	now := m.currentTime()
	previous := m.transcriptSelection.lastClick
	count := 1
	if hasWord && previous != nil && now.Sub(previous.at) <= transcriptSelectionMultiClickInterval &&
		previous.row == point.row && previous.wordStart == word.start.col && previous.wordEnd == word.end.col {
		count = previous.count%3 + 1
	}
	if !hasWord {
		m.transcriptSelection.lastClick = nil
		return count
	}
	m.transcriptSelection.lastClick = &transcriptSelectionClick{
		at: now, count: count, row: point.row, wordStart: word.start.col, wordEnd: word.end.col,
	}
	return count
}

func (m *Model) updateTranscriptSelectionFocus(point transcriptSelectionPoint) {
	m.transcriptSelectionRenderedValid = false
	initial := m.transcriptSelection.initial
	if m.transcriptSelection.granularity == transcriptSelectionCharacter || initial == nil {
		m.transcriptSelection.focus = cloneTranscriptSelectionPoint(point)
		return
	}
	var target transcriptSelectionRange
	if m.transcriptSelection.granularity == transcriptSelectionWord {
		var ok bool
		target, ok = m.transcriptWordRange(point)
		if !ok {
			return
		}
	} else {
		target = m.transcriptLineRange(point)
	}
	if transcriptSelectionPointBefore(target.start, initial.start) {
		m.transcriptSelection.anchor = cloneTranscriptSelectionPoint(initial.end)
		m.transcriptSelection.focus = cloneTranscriptSelectionPoint(target.start)
		return
	}
	m.transcriptSelection.anchor = cloneTranscriptSelectionPoint(initial.start)
	m.transcriptSelection.focus = cloneTranscriptSelectionPoint(target.end)
}

func transcriptSelectionPointBefore(left, right transcriptSelectionPoint) bool {
	return left.row < right.row || left.row == right.row && left.col < right.col
}

func (m *Model) transcriptSelectionBounds() (transcriptSelectionRange, bool) {
	anchor, focus := m.transcriptSelection.anchor, m.transcriptSelection.focus
	if anchor == nil || focus == nil || anchor.row == focus.row && anchor.col == focus.col {
		return transcriptSelectionRange{}, false
	}
	if transcriptSelectionPointBefore(*anchor, *focus) {
		return transcriptSelectionRange{start: *anchor, end: *focus}, true
	}
	return transcriptSelectionRange{start: *focus, end: *anchor}, true
}

func (m *Model) transcriptLine(row int) string {
	lines := m.transcriptSelectionSourceLines()
	if row < 0 || row >= len(lines) {
		return ""
	}
	return lines[row]
}

func (m *Model) transcriptLineRange(point transcriptSelectionPoint) transcriptSelectionRange {
	return transcriptSelectionRange{
		start: transcriptSelectionPoint{row: point.row, col: 0},
		end:   transcriptSelectionPoint{row: point.row, col: xansi.StringWidth(m.transcriptLine(point.row)), boundary: true},
	}
}

func transcriptWordKind(cluster string) uint8 {
	r, _ := utf8.DecodeRuneInString(cluster)
	switch {
	case unicode.IsSpace(r):
		return 0
	case unicode.IsLetter(r) || unicode.IsNumber(r) || r == '_':
		return 1
	default:
		return 2
	}
}

func (m *Model) transcriptWordRange(point transcriptSelectionPoint) (transcriptSelectionRange, bool) {
	plain := xansi.Strip(m.transcriptLine(point.row))
	segments := make([]transcriptWordSegment, 0, len(plain))
	column := 0
	for len(plain) > 0 {
		cluster, width := xansi.FirstGraphemeCluster(plain, xansi.GraphemeWidth)
		if len(cluster) == 0 {
			break
		}
		if width < 1 {
			width = 1
		}
		kind := transcriptWordKind(cluster)
		if len(segments) > 0 && segments[len(segments)-1].kind == kind {
			segments[len(segments)-1].end = column + width
		} else {
			segments = append(segments, transcriptWordSegment{start: column, end: column + width, kind: kind})
		}
		column += width
		plain = plain[len(cluster):]
	}
	for _, segment := range segments {
		if point.col >= segment.start && point.col < segment.end {
			return transcriptSelectionRange{
				start: transcriptSelectionPoint{row: point.row, col: segment.start},
				end:   transcriptSelectionPoint{row: point.row, col: segment.end, boundary: true},
			}, true
		}
	}
	return transcriptSelectionRange{}, false
}

func transcriptGraphemeCellRange(line string, column int) (int, int, bool) {
	plain := xansi.Strip(line)
	position := 0
	for len(plain) > 0 {
		cluster, width := xansi.FirstGraphemeCluster(plain, xansi.GraphemeWidth)
		if len(cluster) == 0 {
			break
		}
		if width < 1 {
			width = 1
		}
		if column >= position && column < position+width {
			return position, position + width, true
		}
		position += width
		plain = plain[len(cluster):]
	}
	return position, position, false
}

func transcriptSelectionColumns(line string, row int, selection transcriptSelectionRange) (int, int) {
	lineWidth := xansi.StringWidth(line)
	start, end := 0, lineWidth
	if row == selection.start.row {
		if cellStart, _, ok := transcriptGraphemeCellRange(line, selection.start.col); ok {
			start = cellStart
		} else {
			start = min(max(0, selection.start.col), lineWidth)
		}
	}
	if row == selection.end.row {
		if selection.end.boundary {
			end = min(max(0, selection.end.col), lineWidth)
		} else if _, cellEnd, ok := transcriptGraphemeCellRange(line, selection.end.col); ok {
			end = cellEnd
		} else {
			end = min(max(0, selection.end.col+1), lineWidth)
		}
	}
	return min(start, lineWidth), max(min(end, lineWidth), min(start, lineWidth))
}

func (m *Model) selectedTranscriptText() string {
	selection, ok := m.transcriptSelectionBounds()
	if !ok {
		return ""
	}
	lines := m.transcriptSelectionSourceLines()
	if selection.start.row < 0 || selection.end.row >= len(lines) {
		return ""
	}
	selected := make([]string, 0, selection.end.row-selection.start.row+1)
	for row := selection.start.row; row <= selection.end.row; row++ {
		line := lines[row]
		start, end := transcriptSelectionColumns(line, row, selection)
		text := xansi.Strip(xansi.Cut(line, start, end))
		selected = append(selected, strings.TrimRight(text, " \t"))
	}
	return strings.Join(selected, "\n")
}

func applyTranscriptSelectionHighlight(text string) string {
	if text == "" {
		return text
	}
	var out strings.Builder
	out.Grow(len(text) + 16)
	out.WriteString("\x1b[7m")
	for index := 0; index < len(text); {
		if text[index] != '\x1b' || index+1 >= len(text) || text[index+1] != '[' {
			out.WriteByte(text[index])
			index++
			continue
		}
		end := index + 2
		for end < len(text) && (text[end] < 0x40 || text[end] > 0x7e) {
			end++
		}
		if end >= len(text) {
			out.WriteString(text[index:])
			break
		}
		end++
		out.WriteString(text[index:end])
		if text[end-1] == 'm' {
			out.WriteString("\x1b[7m")
		}
		index = end
	}
	out.WriteString("\x1b[27m")
	return out.String()
}

func (m *Model) cacheTranscriptSelectionView() {
	m.transcriptSelectionView = m.transcript.View()
	m.transcriptSelectionViewRow = m.transcript.YOffset
	m.transcriptSelectionViewValid = true
}

func (m *Model) renderTranscriptView() string {
	if m.transcriptSelection.pressActive && m.transcriptSelectionRenderedValid {
		return m.transcriptSelectionRendered
	}
	view := ""
	if m.transcriptSelectionViewValid && m.transcriptSelectionViewRow == m.transcript.YOffset {
		view = m.transcriptSelectionView
	} else {
		view = m.transcript.View()
		if m.transcriptSelection.pressActive {
			m.transcriptSelectionView = view
			m.transcriptSelectionViewRow = m.transcript.YOffset
			m.transcriptSelectionViewValid = true
		}
	}
	selection, ok := m.transcriptSelectionBounds()
	if !ok || view == "" {
		m.transcriptSelectionRendered = view
		m.transcriptSelectionRenderedValid = true
		return view
	}
	visible := strings.Split(view, "\n")
	for index, line := range visible {
		row := m.transcript.YOffset + index
		if row < selection.start.row || row > selection.end.row {
			continue
		}
		source := m.transcriptLine(row)
		start, end := transcriptSelectionColumns(source, row, selection)
		if end <= start {
			continue
		}
		lineWidth := xansi.StringWidth(line)
		start = min(start, lineWidth)
		end = min(end, lineWidth)
		before := xansi.Cut(line, 0, start)
		selected := xansi.Cut(line, start, end)
		after := xansi.Cut(line, end, lineWidth)
		visible[index] = before + applyTranscriptSelectionHighlight(selected) + after
	}
	rendered := strings.Join(visible, "\n")
	m.transcriptSelectionRendered = rendered
	m.transcriptSelectionRenderedValid = true
	return rendered
}

func (m *Model) updateTranscriptSelectionAutoScroll(x, y int) tea.Cmd {
	top := m.transcriptSelectionTop()
	bottom := top + m.transcript.Height - 1
	direction := 0
	step := 0
	if y <= top {
		direction = -1
		step = (top-y+1)*2 + 2
	} else if y >= bottom {
		direction = 1
		step = (y-bottom+1)*2 + 2
	}
	m.transcriptSelection.autoScrollX = x
	m.transcriptSelection.autoScrollY = y
	// Native terminal selection accelerates as the pointer moves farther beyond
	// the text region. Cell-motion coordinates may be outside the viewport, so
	// use that distance while capping jumps to preserve visual tracking.
	m.transcriptSelection.autoScrollStep = min(max(4, step), max(8, m.transcript.Height/2))
	if direction == 0 {
		m.stopTranscriptSelectionAutoScroll()
		return nil
	}
	if m.transcriptSelection.autoScroll == direction {
		return nil
	}
	m.transcriptSelection.autoScroll = direction
	m.transcriptSelection.autoScrollTicks = 0
	m.transcriptSelection.autoScrollID++
	id := m.transcriptSelection.autoScrollID
	return tea.Tick(transcriptSelectionAutoScrollInterval, func(time.Time) tea.Msg {
		return transcriptSelectionAutoScrollMsg(id)
	})
}

func (m *Model) stopTranscriptSelectionAutoScroll() {
	m.transcriptSelection.autoScroll = 0
	m.transcriptSelection.autoScrollTicks = 0
	m.transcriptSelection.autoScrollID++
}

func (m *Model) handleTranscriptSelectionAutoScroll(id uint64) tea.Cmd {
	selection := &m.transcriptSelection
	if !selection.pressActive || selection.autoScroll == 0 || selection.autoScrollID != id {
		return nil
	}
	before := m.transcript.YOffset
	// Terminal mouse coordinates commonly clamp to the last visible row, so
	// pointer distance alone cannot accelerate further. Ramp with dwell time as
	// native terminal selection does, eventually moving one viewport per frame.
	selection.autoScrollTicks++
	step := min(max(1, selection.autoScrollStep+selection.autoScrollTicks/2), max(1, m.transcript.Height))
	if selection.autoScroll < 0 {
		m.transcript.ScrollUp(step)
	} else {
		m.transcript.ScrollDown(step)
		m.catchUpTranscriptAtBottom()
	}
	if m.transcript.YOffset == before {
		m.stopTranscriptSelectionAutoScroll()
		return nil
	}
	if point, ok := m.transcriptSelectionPointAt(selection.autoScrollX, selection.autoScrollY, true); ok {
		m.updateTranscriptSelectionFocus(point)
		m.transcriptSelectionRenderedValid = false
	}
	return tea.Tick(transcriptSelectionAutoScrollInterval, func(time.Time) tea.Msg {
		return transcriptSelectionAutoScrollMsg(id)
	})
}
