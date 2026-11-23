// Package ui is the popup: the list of prompts on the left, the selected
// prompt's report on the right, and the handoff to the editor.
package ui

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"

	"herdr-prompt-history/internal/herdr"
	"herdr-prompt-history/internal/transcript"
)

// Options is what the viewer needs to start.
type Options struct {
	Path      string // the transcript file
	SessionID string
	PaneID    string // the Claude pane, for its working status; may be ""
	Config    Config
	Dark      bool // the terminal has a dark background
}

// Run shows the viewer until the person closes it.
func Run(opt Options) error {
	_, err := tea.NewProgram(newModel(opt), tea.WithAltScreen(), tea.WithMouseCellMotion()).Run()
	return err
}

const (
	pollEvery   = 500 * time.Millisecond
	statusEvery = 4 // in polls
	wideFrom    = 100
	rowLines    = 2
	wheelLines  = 3
	searchLines = 2 // the search line and the rule under it, atop the list
)

type pane int

const (
	paneList pane = iota
	paneReport
)

type (
	tickMsg       time.Time
	statusMsg     string
	editorDoneMsg struct{ err error }
)

type model struct {
	opt   Options
	tail  *transcript.Tailer
	all   []*transcript.Turn
	shown []*transcript.Turn

	cursor, top int
	focus       pane
	full        bool
	filter      textinput.Model
	filtering   bool

	vp            viewport.Model
	width, height int
	ticks         int
	status        string
	flash         string
	flashUntil    time.Time
	loadErr       error

	renderer      *glamour.TermRenderer
	rendererWidth int
	shownKey      string
}

func newModel(opt Options) *model {
	filter := textinput.New()
	filter.Prompt = "/"
	filter.Placeholder = "filter prompts"
	filter.CharLimit = 200
	m := &model{
		opt:    opt,
		tail:   transcript.NewTailer(opt.Path),
		filter: filter,
		vp:     viewport.New(0, 0),
	}
	if _, err := m.tail.Poll(); err != nil {
		m.loadErr = err
	}
	m.all = m.tail.Parser.Turns()
	m.shown = m.all
	m.cursor = max(0, len(m.shown)-1)
	return m
}

func (m *model) Init() tea.Cmd {
	return tea.Batch(tick(), m.fetchStatus())
}

func tick() tea.Cmd {
	return tea.Tick(pollEvery, func(t time.Time) tea.Msg { return tickMsg(t) })
}

func (m *model) fetchStatus() tea.Cmd {
	id := m.opt.PaneID
	if id == "" {
		return nil
	}
	return func() tea.Msg {
		p, err := herdr.GetPane(id)
		if err != nil {
			return statusMsg("")
		}
		return statusMsg(p.Status)
	}
}

func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.layout()
		m.syncReport(false)
		return m, nil

	case tickMsg:
		cmds := []tea.Cmd{tick()}
		changed, err := m.tail.Poll()
		m.loadErr = err
		if changed {
			m.reload()
		}
		m.ticks++
		if m.ticks%statusEvery == 0 {
			cmds = append(cmds, m.fetchStatus())
		}
		return m, tea.Batch(cmds...)

	case statusMsg:
		m.status = string(msg)
		return m, nil

	case editorDoneMsg:
		if msg.err != nil {
			m.say("editor: " + msg.err.Error())
		}
		return m, nil

	case tea.MouseMsg:
		m.updateMouse(msg)
		return m, nil

	case tea.KeyMsg:
		if m.filtering {
			return m.updateFilter(msg)
		}
		return m.updateKey(msg)
	}
	return m, nil
}

func (m *model) updateFilter(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.filter.SetValue("")
		m.stopFiltering()
		return m, nil
	case "enter":
		m.stopFiltering()
		return m, nil
	case "ctrl+c":
		return m, tea.Quit
	}
	var cmd tea.Cmd
	m.filter, cmd = m.filter.Update(msg)
	m.applyFilter(nil, true)
	return m, cmd
}

func (m *model) stopFiltering() {
	m.filtering = false
	m.filter.Blur()
	m.applyFilter(m.selected(), false)
}

func (m *model) updateKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "q", "ctrl+c":
		return m, tea.Quit
	case "esc":
		if m.filter.Value() != "" {
			m.filter.SetValue("")
			m.applyFilter(m.selected(), false)
			return m, nil
		}
		return m, tea.Quit
	case "j", "down":
		if m.focus == paneList {
			m.move(1)
		} else {
			m.vp.ScrollDown(1)
		}
	case "k", "up":
		if m.focus == paneList {
			m.move(-1)
		} else {
			m.vp.ScrollUp(1)
		}
	case "g", "home":
		if m.focus == paneList {
			m.move(-len(m.shown))
		} else {
			m.vp.GotoTop()
		}
	case "G", "end":
		if m.focus == paneList {
			m.move(len(m.shown))
		} else {
			m.vp.GotoBottom()
		}
	case "ctrl+d", "d", " ":
		m.vp.HalfPageDown()
	case "ctrl+u", "u":
		m.vp.HalfPageUp()
	case "ctrl+f", "pgdown":
		m.vp.PageDown()
	case "ctrl+b", "pgup":
		m.vp.PageUp()
	case "ctrl+e":
		m.vp.ScrollDown(1)
	case "ctrl+y":
		m.vp.ScrollUp(1)
	case "tab":
		if m.focus == paneList {
			m.focus = paneReport
		} else {
			m.focus = paneList
		}
	case "l", "right":
		m.focus = paneReport
	case "h", "left":
		m.focus = paneList
	case "/":
		m.filtering = true
		m.focus = paneList
		return m, m.filter.Focus()
	case "t":
		m.full = !m.full
		m.syncReport(true)
	case "enter", "o":
		return m, m.openInEditor()
	case "y":
		m.copyReport()
	}
	return m, nil
}

// paneAt says which pane a screen column belongs to.
func (m *model) paneAt(x int) pane {
	if !m.wide() {
		return m.focus
	}
	if x < m.listWidth() {
		return paneList
	}
	return paneReport
}

// updateMouse scrolls the pane under the pointer with the wheel, and focuses
// a pane (selecting the prompt under the pointer) with a click.
func (m *model) updateMouse(msg tea.MouseMsg) {
	if msg.Action != tea.MouseActionPress {
		return
	}
	target := m.paneAt(msg.X)
	switch msg.Button {
	case tea.MouseButtonWheelUp:
		if target == paneList {
			m.move(-1)
		} else {
			m.vp.ScrollUp(wheelLines)
		}
	case tea.MouseButtonWheelDown:
		if target == paneList {
			m.move(1)
		} else {
			m.vp.ScrollDown(wheelLines)
		}
	case tea.MouseButtonLeft:
		m.focus = target
		// Rows start under the header line, the box's top border and the
		// search line with its rule.
		if row := msg.Y - 2 - searchLines; target == paneList && row >= 0 {
			if i := m.top + row/rowLines; i < len(m.shown) {
				m.cursor = i
				m.syncReport(false)
			}
		}
	}
}

func (m *model) selected() *transcript.Turn {
	if m.cursor < 0 || m.cursor >= len(m.shown) {
		return nil
	}
	return m.shown[m.cursor]
}

func (m *model) move(by int) {
	if len(m.shown) == 0 {
		return
	}
	m.cursor = min(max(m.cursor+by, 0), len(m.shown)-1)
	m.syncReport(false)
}

// reload takes the parser's turns after the transcript grew, keeping the
// selection, or following the newest prompt when it was selected.
func (m *model) reload() {
	following := m.cursor == len(m.shown)-1
	keep := m.selected()
	m.all = m.tail.Parser.Turns()
	m.applyFilter(keep, following)
}

func (m *model) applyFilter(keep *transcript.Turn, following bool) {
	query := strings.ToLower(strings.TrimSpace(m.filter.Value()))
	if query == "" {
		m.shown = m.all
	} else {
		m.shown = m.shown[:0:0]
		for _, t := range m.all {
			if strings.Contains(strings.ToLower(t.Prompt), query) {
				m.shown = append(m.shown, t)
			}
		}
	}
	m.cursor = len(m.shown) - 1
	if !following && keep != nil {
		for i, t := range m.shown {
			if t == keep {
				m.cursor = i
				break
			}
		}
	}
	m.cursor = max(m.cursor, 0)
	m.syncReport(false)
}

func (m *model) say(text string) {
	m.flash = text
	m.flashUntil = time.Now().Add(3 * time.Second)
}

func (m *model) openInEditor() tea.Cmd {
	t := m.selected()
	if t == nil {
		return nil
	}
	path, err := writeReport(m.opt.SessionID, t, m.full)
	if err != nil {
		m.say("could not write the report: " + err.Error())
		return nil
	}
	editor, err := m.opt.Config.editor()
	if err != nil {
		m.say(err.Error())
		return nil
	}
	cmd := exec.Command(editor, path)
	cmd.Env = append(os.Environ(), m.opt.Config.NvimEnv...)
	return tea.ExecProcess(cmd, func(err error) tea.Msg { return editorDoneMsg{err} })
}

func (m *model) copyReport() {
	t := m.selected()
	if t == nil {
		return
	}
	if err := copyToClipboard(answerOf(t, m.full)); err != nil {
		m.say("copy failed: " + err.Error())
		return
	}
	m.say(fmt.Sprintf("copied the report for #%d", t.N))
}

func (m *model) running(t *transcript.Turn) bool {
	return m.status == "working" && len(m.all) > 0 && m.all[len(m.all)-1] == t
}

// ---- layout ----

func (m *model) wide() bool { return m.width >= wideFrom }

// bodyHeight is the height of the two boxes, borders included.
func (m *model) bodyHeight() int { return max(m.height-2, 3) }

// listWidth and reportWidth are the boxes' widths, borders included.
func (m *model) listWidth() int {
	if !m.wide() {
		return m.width
	}
	return min(max(m.width*36/100, 34), 64)
}

func (m *model) reportWidth() int {
	if !m.wide() {
		return m.width
	}
	return m.width - m.listWidth()
}

func (m *model) layout() {
	// Inside the border, one column of padding on each side.
	m.vp.Width = max(m.reportWidth()-4, 10)
	m.vp.Height = max(m.bodyHeight()-2, 1)
	// The list's inner width, less the input's prompt and cursor.
	m.filter.Width = max(m.listWidth()-2-4, 5)
}

func (m *model) listRows() int {
	return max((m.bodyHeight()-2-searchLines)/rowLines, 1)
}

// ---- report ----

func (m *model) markdown(text string, width int) string {
	if m.renderer == nil || m.rendererWidth != width {
		style := "dark"
		if !m.opt.Dark {
			style = "light"
		}
		r, err := glamour.NewTermRenderer(
			glamour.WithStandardStyle(style),
			glamour.WithWordWrap(width),
		)
		if err != nil {
			return text
		}
		m.renderer, m.rendererWidth = r, width
	}
	out, err := m.renderer.Render(text)
	if err != nil {
		return text
	}
	return strings.Trim(out, "\n")
}

// syncReport redraws the report when the selection, its content, the mode or
// the width changed; toTop also scrolls it back to the start.
func (m *model) syncReport(toTop bool) {
	if m.vp.Width <= 0 {
		return
	}
	t := m.selected()
	if t == nil {
		m.shownKey = ""
		m.vp.SetContent(dim.Render("No prompt selected."))
		return
	}
	key := fmt.Sprintf("%p|%v|%d|%d|%v", t, m.full, m.vp.Width, len(t.Items), m.running(t))
	turnKey := fmt.Sprintf("%p|%v", t, m.full)
	if key == m.shownKey {
		return
	}
	if !strings.HasPrefix(m.shownKey, turnKey+"|") {
		toTop = true
	}
	m.shownKey = key

	w := m.vp.Width
	var b strings.Builder
	title := fmt.Sprintf("#%d", t.N)
	if !t.Time.IsZero() {
		title += " · " + t.Time.Local().Format("Mon 2 Jan 15:04")
	}
	b.WriteString(bold.Width(w).Render(title) + "\n")
	if label := kindLabel(t.Kind); label != "" {
		b.WriteString(dim.Width(w).Render(label) + "\n")
	}
	b.WriteString(promptStyle.Width(w).Render(clipLines(t.Prompt, 8)))
	b.WriteString("\n")
	stats := turnStats(t)
	if m.running(t) {
		stats = "working… · " + stats
	}
	if m.full {
		stats += " · full turn"
	} else {
		stats += " · final answer"
	}
	b.WriteString(dim.Width(w).Render(stats) + "\n")
	b.WriteString(dim.Render(strings.Repeat("─", w)) + "\n")
	answer := answerOf(t, m.full)
	if strings.TrimSpace(answer) == "" {
		b.WriteString(dim.Render("No answer yet."))
	} else {
		b.WriteString(m.markdown(answer, w))
	}
	m.vp.SetContent(b.String())
	if toTop {
		m.vp.GotoTop()
	}
}

func clipLines(s string, limit int) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	if len(lines) > limit {
		lines = append(lines[:limit], "…")
	}
	return strings.Join(lines, "\n")
}

// ---- view ----

var (
	accent      = lipgloss.AdaptiveColor{Light: "25", Dark: "111"}
	bold        = lipgloss.NewStyle().Bold(true)
	dim         = lipgloss.NewStyle().Faint(true)
	accentText  = lipgloss.NewStyle().Foreground(accent)
	promptStyle = lipgloss.NewStyle().Italic(true)
	boxStyle    = lipgloss.NewStyle().Border(lipgloss.RoundedBorder())
)

func box(focused bool, width, height int, content string) string {
	s := boxStyle.Width(width - 2).Height(height - 2).MaxHeight(height)
	if focused {
		s = s.BorderForeground(accent)
	} else {
		s = s.BorderForeground(lipgloss.Color("8"))
	}
	return s.Render(content)
}

func (m *model) View() string {
	if m.width == 0 {
		return ""
	}
	if m.width < 30 || m.height < 8 {
		return "Window too small."
	}
	var body string
	switch {
	case m.wide():
		body = lipgloss.JoinHorizontal(lipgloss.Top,
			box(m.focus == paneList, m.listWidth(), m.bodyHeight(), m.listView()),
			box(m.focus == paneReport, m.reportWidth(), m.bodyHeight(), m.reportView()),
		)
	case m.focus == paneReport:
		body = box(true, m.width, m.bodyHeight(), m.reportView())
	default:
		body = box(true, m.width, m.bodyHeight(), m.listView())
	}
	return m.headerView() + "\n" + body + "\n" + m.footerView()
}

func (m *model) headerView() string {
	left := " " + bold.Render("Prompt history")
	count := plural(len(m.all), "prompt")
	if len(m.shown) != len(m.all) {
		count = fmt.Sprintf("%d of %s", len(m.shown), count)
	}
	left += dim.Render(" · " + count)
	right := ""
	switch {
	case m.loadErr != nil:
		right = "cannot read the transcript: " + m.loadErr.Error()
	case m.status != "":
		right = "Claude: " + m.status
	}
	right = dim.Render(runewidth.Truncate(right, max(m.width/2, 10), "…")) + " "
	gap := m.width - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 1 {
		return left
	}
	return left + strings.Repeat(" ", gap) + right
}

func (m *model) footerView() string {
	if m.filtering {
		return " " + dim.Render("type to filter · enter keep the filter · esc clear it")
	}
	if m.flash != "" && time.Now().Before(m.flashUntil) {
		return " " + accentText.Render(m.flash)
	}
	keys := "j/k move · tab switch pane · d/u scroll report · / filter · t final/full · enter open in nvim · y copy · q close"
	if m.focus == paneReport {
		keys = "j/k scroll · d/u half page · g/G top/bottom · wheel · tab back to list · t final/full · enter open in nvim · y copy · q close"
	}
	return " " + dim.Render(runewidth.Truncate(keys, max(m.width-2, 1), "…"))
}

// searchView is the search line at the top of the list, with a rule under it.
func (m *model) searchView(w int) string {
	var line string
	switch {
	case m.filtering:
		line = " " + m.filter.View()
	case m.filter.Value() != "":
		line = " " + accentText.Render(runewidth.Truncate("/"+m.filter.Value(), w-2, "…"))
	default:
		line = " " + dim.Render(runewidth.Truncate("/ to filter prompts", w-2, "…"))
	}
	return line + "\n" + dim.Render(strings.Repeat("─", w))
}

func (m *model) listView() string {
	w := m.listWidth() - 2
	search := m.searchView(w) + "\n"
	if len(m.shown) == 0 {
		if len(m.all) == 0 {
			return search + dim.Render(" No prompts in this session yet.")
		}
		return search + dim.Render(" No prompt matches the filter.")
	}
	rows := m.listRows()
	if m.cursor < m.top {
		m.top = m.cursor
	}
	if m.cursor >= m.top+rows {
		m.top = m.cursor - rows + 1
	}
	m.top = min(m.top, max(len(m.shown)-rows, 0))

	var lines []string
	for i := m.top; i < len(m.shown) && i < m.top+rows; i++ {
		t := m.shown[i]
		isSel := i == m.cursor
		mark := " "
		if isSel {
			mark = "▌"
		}
		clock := "     "
		if !t.Time.IsZero() {
			clock = t.Time.Local().Format("15:04")
		}
		text := t.FirstLine()
		if t.Kind == transcript.KindQueued || t.Kind == transcript.KindReject {
			text = "↳ " + text
		}
		head := fmt.Sprintf("#%-3d %s  %s", t.N, clock, text)
		head = pad(runewidth.Truncate(head, w-1, "…"), w-1)
		stats := turnStats(t)
		if m.running(t) {
			stats = "● working · " + stats
		}
		sub := pad(runewidth.Truncate("     "+stats, w-1, "…"), w-1)

		switch {
		case isSel && m.focus == paneList:
			lines = append(lines,
				accentText.Render(mark)+lipgloss.NewStyle().Reverse(true).Bold(true).Render(head),
				accentText.Render(mark)+dim.Render(sub))
		case isSel:
			lines = append(lines,
				accentText.Render(mark)+bold.Render(head),
				accentText.Render(mark)+dim.Render(sub))
		default:
			lines = append(lines, mark+head, mark+dim.Render(sub))
		}
	}
	return search + strings.Join(lines, "\n")
}

func (m *model) reportView() string {
	return lipgloss.NewStyle().Padding(0, 1).Render(m.vp.View())
}

func pad(s string, width int) string {
	if gap := width - runewidth.StringWidth(s); gap > 0 {
		return s + strings.Repeat(" ", gap)
	}
	return s
}
