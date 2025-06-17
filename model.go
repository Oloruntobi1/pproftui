package main

import (
	"fmt"
	"time"

	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// Implements list.Item for our FunctionProfile
type item struct {
	prof FunctionProfile
}

func (i item) Title() string       { return i.prof.Name }
func (i item) Description() string { return fmt.Sprintf("Flat: %s", formatNanos(i.prof.FlatValue)) }
func (i item) FilterValue() string { return i.prof.Name }

// formatNanos converts nanoseconds to a more readable time format.
func formatNanos(n int64) string {
	d := time.Duration(n)
	return d.String()
}

type model struct {
	functions []FunctionProfile
	list      list.Model
	source    viewport.Model
	styles    Styles
	ready     bool
}

func newModel(functions []FunctionProfile) model {
	items := make([]list.Item, len(functions))
	for i, f := range functions {
		items[i] = item{prof: f}
	}

	styles := defaultStyles()

	m := model{
		functions: functions,
		list:      list.New(items, list.NewDefaultDelegate(), 0, 0),
		source:    viewport.New(0, 0),
		styles:    styles,
	}

	m.list.Title = "Profiled Functions (Heaviest First)"
	m.source.Style = styles.Source

	return m
}

func (m model) Init() tea.Cmd {
	return nil
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		h, v := m.styles.Base.GetFrameSize()

		// Split the screen: 40% for list, 60% for source
		listWidth := int(float64(msg.Width-h) * 0.4)
		sourceWidth := msg.Width - h - listWidth

		m.list.SetSize(listWidth, msg.Height-v-3) // Leave space for status bar
		m.styles.List = m.styles.List.Width(listWidth).Height(msg.Height - v - 3)
		m.styles.Source = m.styles.Source.Width(sourceWidth).Height(msg.Height - v - 3)
		m.source.Width = sourceWidth
		m.source.Height = msg.Height - v - 3

		if !m.ready {
			m.ready = true
			m.updateSourceView() // Initial source view
		}
	case tea.KeyMsg:
		if msg.String() == "ctrl+c" || msg.String() == "q" {
			return m, tea.Quit
		}
	}

	beforeIndex := m.list.Index()

	var listCmd, sourceCmd tea.Cmd
	m.list, listCmd = m.list.Update(msg)
	m.source, sourceCmd = m.source.Update(msg)
	cmds = append(cmds, listCmd, sourceCmd)

	if beforeIndex != m.list.Index() {
		m.updateSourceView()
	}

	return m, tea.Batch(cmds...)
}

func (m *model) updateSourceView() {
	selected, ok := m.list.SelectedItem().(item)
	if !ok {
		return
	}

	content := getHighlightedSource(selected.prof.FileName, selected.prof.StartLine)
	m.source.SetContent(content)

	targetLine := selected.prof.StartLine
	halfViewportHeight := m.source.Height / 2

	newYOffset := targetLine - halfViewportHeight

	if newYOffset < 0 {
		newYOffset = 0
	}

	m.source.SetYOffset(newYOffset)
}

func (m model) View() string {
	if !m.ready {
		return "Initializing..."
	}

	// Status bar
	statusText := m.styles.Status.Render(
		fmt.Sprintf("Total Functions: %d | Use ↑/↓ to navigate, q to quit", len(m.functions)),
	)

	// Combine panes
	panes := lipgloss.JoinHorizontal(lipgloss.Top,
		m.styles.List.Render(m.list.View()),
		m.styles.Source.Render(m.source.View()),
	)

	return m.styles.Base.Render(lipgloss.JoinVertical(lipgloss.Left, panes, statusText))
}
