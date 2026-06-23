package tui

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/sahilm/fuzzy"

	"github.com/imthor/session-search/internal/core"
)

// RunTUI runs the interactive fzf-like browser with right preview pane.
// It returns the selected session or nil if cancelled.
func RunTUI(sessions []core.Session, initialQuery string) (*core.Session, error) {
	m := newModel(sessions, initialQuery)
	p := tea.NewProgram(m, tea.WithAltScreen(), tea.WithMouseCellMotion())
	final, err := p.Run()
	if err != nil {
		return nil, err
	}
	if fm, ok := final.(model); ok && fm.selected != nil {
		return fm.selected, nil
	}
	return nil, nil
}

type sessionItem struct {
	session core.Session
	title   string
	desc    string
}

func (i sessionItem) Title() string       { return i.title }
func (i sessionItem) Description() string { return i.desc }
func (i sessionItem) FilterValue() string {
	return i.session.Project + " " + i.session.Preview + " " + i.session.Blurb
}

type model struct {
	all         []core.Session
	list        list.Model
	input       textinput.Model
	preview     viewport.Model
	selected    *core.Session
	quitting    bool
	query       string
	width       int
	height      int
	previewPath string // to avoid reloading same file unnecessarily
}

var (
	titleStyle    = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("205"))
	dimStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
	matchStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("11")).Bold(true) // yellow highlight
	statusStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	previewStyle  = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("62")).Padding(0, 1)
	leftPaneStyle = lipgloss.NewStyle().Border(lipgloss.NormalBorder()).BorderForeground(lipgloss.Color("238"))
)

func newModel(sessions []core.Session, initial string) model {
	ti := textinput.New()
	ti.Placeholder = "fuzzy search... (type to filter)"
	ti.Focus()
	ti.CharLimit = 200
	ti.Width = 50
	if initial != "" {
		ti.SetValue(initial)
	}

	// Create list
	items := buildItems(sessions, "")
	delegate := list.NewDefaultDelegate()
	delegate.Styles.NormalTitle = lipgloss.NewStyle().Foreground(lipgloss.Color("252"))
	delegate.Styles.SelectedTitle = lipgloss.NewStyle().Foreground(lipgloss.Color("205")).Bold(true)
	delegate.Styles.NormalDesc = dimStyle
	delegate.Styles.SelectedDesc = lipgloss.NewStyle().Foreground(lipgloss.Color("245"))

	l := list.New(items, delegate, 60, 20)
	l.Title = "Sessions"
	l.SetShowStatusBar(false)
	l.SetFilteringEnabled(false) // we do custom filtering
	l.SetShowHelp(false)

	vp := viewport.New(40, 20)
	vp.SetContent("Select a session to see details and matched text here.")

	m := model{
		all:     sessions,
		list:    l,
		input:   ti,
		preview: vp,
	}
	m.filter()
	return m
}

func buildItems(sessions []core.Session, query string) []list.Item {
	items := make([]list.Item, len(sessions))
	for i, s := range sessions {
		title := fmt.Sprintf("%s  %s", timeAgo(s.Start), truncate(s.Preview, 80))
		if query != "" {
			title = highlightSimple(title, query)
		}
		desc := fmt.Sprintf("%s • %s", s.Project, filepath.Base(s.Path))
		items[i] = sessionItem{
			session: s,
			title:   title,
			desc:    desc,
		}
	}
	return items
}

func (m model) Init() tea.Cmd {
	return textinput.Blink
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd
	var cmd tea.Cmd

	prevQuery := m.query

	switch msg := msg.(type) {
	case tea.KeyMsg:
		k := msg.String()
		// Handle ALL navigation first, BEFORE any input update, so they never edit the query.
		if isNavigationKey(k) {
			switch k {
			case "ctrl+c", "esc":
				m.quitting = true
				return m, tea.Quit
			case "enter":
				if i, ok := m.list.SelectedItem().(sessionItem); ok {
					m.selected = &i.session
					m.quitting = true
					return m, tea.Quit
				}
			// Preview scroll keys (direct, no keymap reliance)
			case "pgup", "ctrl+b":
				m.preview.PageUp()
				return m, nil
			case "pgdown", "ctrl+f":
				m.preview.PageDown()
				return m, nil
			case "ctrl+u":
				m.preview.HalfPageUp()
				return m, nil
			case "ctrl+d":
				m.preview.HalfPageDown()
				return m, nil
			// List nav (direct calls; bypasses list's limited keymap for ctrl variants)
			case "up", "ctrl+p", "ctrl+k":
				m.list.CursorUp()
				if sel, ok := m.list.SelectedItem().(sessionItem); ok {
					m.preview.SetContent(m.buildPreview(sel.session))
					m.preview.GotoTop()
				}
				return m, nil
			case "down", "ctrl+n", "ctrl+j":
				m.list.CursorDown()
				if sel, ok := m.list.SelectedItem().(sessionItem); ok {
					m.preview.SetContent(m.buildPreview(sel.session))
					m.preview.GotoTop()
				}
				return m, nil
			// Other nav (arrows etc) - let list handle via Update, but we already caught input
			default:
				// For arrows/home/end etc, forward to list
				m.list, cmd = m.list.Update(msg)
				cmds = append(cmds, cmd)
				// refresh preview
				if sel, ok := m.list.SelectedItem().(sessionItem); ok {
					m.preview.SetContent(m.buildPreview(sel.session))
					m.preview.GotoTop()
				}
				return m, tea.Batch(cmds...)
			}
		}
		// Non-navigation: safe to give to input for typing/filter
		m.input, cmd = m.input.Update(msg)
		cmds = append(cmds, cmd)
		newQ := strings.TrimSpace(m.input.Value())
		if newQ != prevQuery {
			m.filter()
		}
		// Do not forward non-nav to list (prevents j/k etc from navigating while typing)

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		listWidth := msg.Width * 55 / 100
		previewWidth := msg.Width - listWidth - 4
		listHeight := msg.Height - 6
		if listHeight < 5 {
			listHeight = 5
		}
		m.list.SetSize(listWidth, listHeight)
		m.preview.Width = previewWidth
		m.preview.Height = listHeight - 2
		if m.preview.Height < 3 {
			m.preview.Height = 3
		}
		// Re-wrap current preview on resize
		if sel, ok := m.list.SelectedItem().(sessionItem); ok {
			m.preview.SetContent(m.buildPreview(sel.session))
		}

	case tea.MouseMsg:
		m.list, cmd = m.list.Update(msg)
		cmds = append(cmds, cmd)
		m.preview, cmd = m.preview.Update(msg)
		cmds = append(cmds, cmd)
	}

	// Refresh preview on selection change
	if sel, ok := m.list.SelectedItem().(sessionItem); ok {
		if m.previewPath != sel.session.Path {
			m.previewPath = sel.session.Path
			m.preview.SetContent(m.buildPreview(sel.session))
			m.preview.GotoTop()
		}
	}

	m.query = strings.TrimSpace(m.input.Value())

	return m, tea.Batch(cmds...)
}

func (m *model) filter() {
	q := strings.TrimSpace(m.input.Value())
	m.query = q

	var filtered []core.Session
	if q == "" {
		filtered = m.all
	} else {
		hay := make([]string, len(m.all))
		for i, s := range m.all {
			hay[i] = s.Project + " " + s.Preview + " " + s.Blurb
		}
		matches := fuzzy.Find(q, hay)
		filtered = make([]core.Session, len(matches))
		for i, mm := range matches {
			filtered[i] = m.all[mm.Index]
		}
	}

	// Sort to keep project groups together for visual grouping (ProjectKey, then recency)
	sort.Slice(filtered, func(i, j int) bool {
		if filtered[i].ProjectKey != filtered[j].ProjectKey {
			return filtered[i].ProjectKey < filtered[j].ProjectKey
		}
		return filtered[i].Start.After(filtered[j].Start)
	})

	items := buildItems(filtered, q)
	m.list.SetItems(items)

	// Update preview for current selection
	if len(items) > 0 {
		if sel, ok := m.list.SelectedItem().(sessionItem); ok {
			m.preview.SetContent(m.buildPreview(sel.session))
			m.previewPath = sel.session.Path
		}
	} else {
		m.preview.SetContent("No matches.")
	}
}

func (m model) buildPreview(s core.Session) string {
	var b strings.Builder

	b.WriteString(lipgloss.NewStyle().Bold(true).Render("Project: ") + s.Project + "\n")
	b.WriteString(lipgloss.NewStyle().Bold(true).Render("Time: ") + s.Start.Format("2006-01-02 15:04:05") + "\n")
	b.WriteString(lipgloss.NewStyle().Bold(true).Render("Path: ") + dimStyle.Render(s.Path) + "\n\n")

	// Try to get actual matched reference from the file
	matched := getMatchedReference(s.Path, m.query)
	if matched != "" {
		b.WriteString(lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("11")).Render("Matched text:") + "\n")
		plainWrapped := wrapText(matched, m.preview.Width-2)
		b.WriteString(highlightSimple(plainWrapped, m.query))
	} else {
		b.WriteString(lipgloss.NewStyle().Bold(true).Render("Preview:") + "\n")
		plainWrapped := wrapText(s.Blurb, m.preview.Width-2)
		b.WriteString(highlightSimple(plainWrapped, m.query))
	}

	return b.String()
}

func getMatchedReference(path string, query string) string {
	if query == "" {
		return ""
	}
	qLower := strings.ToLower(query)
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()

	reader := bufio.NewReader(f)
	var collected []string
	count := 0
	for {
		line, err := reader.ReadString('\n')
		if len(line) > 0 {
			line = strings.TrimRight(line, "\n\r")
			if strings.Contains(strings.ToLower(line), qLower) {
				if content := extractReadableContent(line); content != "" {
					collected = append(collected, content)
				} else {
					collected = append(collected, line) // full for matched reference; viewport scrolls
				}
				count++
				if count >= 4 {
					break
				}
			}
		}
		if err != nil {
			break
		}
	}

	if len(collected) == 0 {
		return ""
	}
	return strings.Join(collected, "\n---\n")
}

func extractReadableContent(line string) string {
	// Best effort: look for common fields in the JSONL
	var ev map[string]any
	if err := json.Unmarshal([]byte(line), &ev); err != nil {
		return ""
	}

	if typ, _ := ev["type"].(string); typ == "user" || typ == "assistant" {
		if msg, ok := ev["message"].(map[string]any); ok {
			if c := extractContent(msg["content"]); c != "" {
				return c // full content, no truncate
			}
		}
	}
	if disp, ok := ev["display"].(string); ok && disp != "" {
		return disp // full
	}
	return ""
}

func extractContent(v any) string {
	if v == nil {
		return ""
	}
	switch x := v.(type) {
	case string:
		return x
	case []any:
		var parts []string
		for _, p := range x {
			if m, ok := p.(map[string]any); ok {
				if t, _ := m["type"].(string); t == "text" {
					if txt, ok := m["text"].(string); ok {
						parts = append(parts, txt)
					}
				}
			}
		}
		return strings.Join(parts, " ")
	}
	return ""
}

func (m model) View() string {
	if m.quitting {
		return ""
	}

	header := titleStyle.Render("session-search") + dimStyle.Render("  —  fuzzy Claude session search (right pane for match details)")

	inputView := m.input.View()

	// Left list pane
	listView := m.list.View()
	left := leftPaneStyle.Width(m.width * 55 / 100).Height(m.list.Height() + 2).Render(inputView + "\n" + listView)

	// Right preview
	previewContent := m.preview.View()
	right := previewStyle.Width(m.width - (m.width * 55 / 100) - 4).Render(previewContent)

	mainArea := lipgloss.JoinHorizontal(lipgloss.Top, left, right)

	status := fmt.Sprintf("%d/%d  •  ↑↓ move  •  enter select  •  type to filter  •  esc cancel", m.list.Index()+1, len(m.list.Items()))
	if m.query != "" {
		status += "  •  query: " + m.query
	}

	return lipgloss.JoinVertical(lipgloss.Top,
		header,
		mainArea,
		statusStyle.Render(status),
	)
}

func highlightSimple(text, query string) string {
	if query == "" {
		return text
	}
	runes := []rune(text)
	lowerRunes := []rune(strings.ToLower(text))
	qRunes := []rune(strings.ToLower(query))
	if len(qRunes) == 0 {
		return text
	}
	for i := 0; i <= len(lowerRunes)-len(qRunes); i++ {
		match := true
		for j := 0; j < len(qRunes); j++ {
			if lowerRunes[i+j] != qRunes[j] {
				match = false
				break
			}
		}
		if match {
			before := string(runes[:i])
			matchPart := string(runes[i : i+len(qRunes)])
			after := string(runes[i+len(qRunes):])
			return before + matchStyle.Render(matchPart) + after
		}
	}
	// word fallback
	for _, w := range strings.Fields(query) {
		if len(w) <= 2 {
			continue
		}
		wRunes := []rune(strings.ToLower(w))
		for i := 0; i <= len(lowerRunes)-len(wRunes); i++ {
			match := true
			for j := 0; j < len(wRunes); j++ {
				if lowerRunes[i+j] != wRunes[j] {
					match = false
					break
				}
			}
			if match {
				before := string(runes[:i])
				matchPart := string(runes[i : i+len(wRunes)])
				after := string(runes[i+len(wRunes):])
				return before + matchStyle.Render(matchPart) + after
			}
		}
	}
	return text
}

func timeAgo(t time.Time) string {
	d := time.Since(t)
	if d < time.Minute {
		return "now"
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm", int(d.Minutes()))
	}
	if d < 24*time.Hour {
		return fmt.Sprintf("%dh", int(d.Hours()))
	}
	return t.Format("01-02")
}

func truncate(s string, n int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}

func wrapText(s string, width int) string {
	if width <= 0 {
		return s
	}
	var lines []string
	for _, line := range strings.Split(s, "\n") {
		runes := []rune(line)
		for len(runes) > width {
			lines = append(lines, string(runes[:width]))
			runes = runes[width:]
		}
		lines = append(lines, string(runes))
	}
	return strings.Join(lines, "\n")
}

func isNavigationKey(k string) bool {
	switch k {
	case "up", "down", "left", "right", "pgup", "pgdown", "home", "end", "tab", "shift+tab", "enter", "space":
		return true
	case "ctrl+p", "ctrl+n", "ctrl+b", "ctrl+f", "ctrl+u", "ctrl+d", "ctrl+k", "ctrl+j":
		return true
	}
	return false
}
