package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/sahilm/fuzzy"

	"github.com/imthor/session-search/internal/core"
)

// RunTUI runs the interactive fzf-like browser.
// It returns the selected session or nil if cancelled.
func RunTUI(sessions []core.Session, initialQuery string) (*core.Session, error) {
	m := newModel(sessions, initialQuery)
	p := tea.NewProgram(m, tea.WithAltScreen())
	final, err := p.Run()
	if err != nil {
		return nil, err
	}
	if fm, ok := final.(model); ok && fm.selected != nil {
		return fm.selected, nil
	}
	return nil, nil
}

type model struct {
	all       []core.Session
	filtered  []core.Session
	cursor    int
	input     textinput.Model
	selected  *core.Session
	quitting  bool
	query     string
	height    int
}

var (
	titleStyle      = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("205"))
	groupStyle      = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("99")).MarginTop(1)
	itemStyle       = lipgloss.NewStyle().PaddingLeft(2)
	selectedStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("205")).Bold(true)
	dimStyle        = lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
	statusStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	previewBorder   = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("238")).Padding(0, 1)
)

func newModel(sessions []core.Session, initial string) model {
	ti := textinput.New()
	ti.Placeholder = "type to fuzzy search..."
	ti.Focus()
	ti.CharLimit = 120
	if initial != "" {
		ti.SetValue(initial)
	}

	m := model{
		all:      sessions,
		filtered: sessions,
		input:    ti,
	}
	m.filter()
	return m
}

func (m model) Init() tea.Cmd { return textinput.Blink }

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "esc":
			m.quitting = true
			return m, tea.Quit
		case "enter":
			if len(m.filtered) > 0 {
				m.selected = &m.filtered[m.cursor]
				m.quitting = true
				return m, tea.Quit
			}
		case "up", "ctrl+p", "ctrl+k":
			if m.cursor > 0 {
				m.cursor--
			}
		case "down", "ctrl+n", "ctrl+j":
			if m.cursor < len(m.filtered)-1 {
				m.cursor++
			}
		default:
			var cmd tea.Cmd
			m.input, cmd = m.input.Update(msg)
			m.filter()
			return m, cmd
		}

	case tea.WindowSizeMsg:
		m.height = msg.Height
	}
	return m, nil
}

func (m *model) filter() {
	q := strings.TrimSpace(m.input.Value())
	m.query = q

	if q == "" {
		m.filtered = m.all
		m.cursor = 0
		return
	}

	hay := make([]string, len(m.all))
	for i, s := range m.all {
		hay[i] = s.Project + " " + s.Preview + " " + s.Blurb
	}

	matches := fuzzy.Find(q, hay)
	m.filtered = make([]core.Session, len(matches))
	for i, mm := range matches {
		m.filtered[i] = m.all[mm.Index]
	}
	m.cursor = 0
}

func (m model) View() string {
	if m.quitting {
		return ""
	}

	var b strings.Builder

	b.WriteString(titleStyle.Render("session-search"))
	b.WriteString(dimStyle.Render("  —  fuzzy find your Claude sessions\n\n"))

	b.WriteString(m.input.View())
	b.WriteString("\n")

	if len(m.filtered) == 0 {
		b.WriteString(dimStyle.Render("\n  no matches\n"))
		return b.String()
	}

	// Grouped rendering
	currentProject := ""
	lines := 0
	maxLines := 22
	if m.height > 14 {
		maxLines = m.height - 10
	}

	for i, s := range m.filtered {
		if lines >= maxLines {
			b.WriteString(dimStyle.Render(fmt.Sprintf("\n... %d more (refine query or use arrows)", len(m.filtered)-i)))
			break
		}
		if s.ProjectKey != currentProject {
			currentProject = s.ProjectKey
			b.WriteString("\n" + groupStyle.Render("▸ "+s.Project) + "\n")
		}

		line := fmt.Sprintf("%s  %s", timeAgo(s.Start), truncate(s.Preview, 75))
		if i == m.cursor {
			line = selectedStyle.Render("→ " + line)
		} else {
			line = itemStyle.Render("  " + line)
		}
		b.WriteString(line + "\n")
		lines++
	}

	status := fmt.Sprintf("%d/%d results  •  enter=select  esc=cancel", len(m.filtered), len(m.all))
	b.WriteString("\n" + statusStyle.Render(status))

	// Preview
	if m.cursor < len(m.filtered) {
		s := m.filtered[m.cursor]
		preview := fmt.Sprintf("%s\n\n%s", dimStyle.Render(s.Path), truncate(s.Blurb, 300))
		b.WriteString("\n\n" + previewBorder.Render(preview))
	}

	return b.String()
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
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}