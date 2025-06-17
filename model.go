// model.go
package main

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// listItem represents a single, view-specific entry in our list.
type listItem struct {
	prof        FunctionProfile
	valueString string
}

func (i listItem) Title() string       { return i.prof.Name }
func (i listItem) Description() string { return i.valueString }
func (i listItem) FilterValue() string { return i.prof.Name }

type model struct {
	profileData      *ProfileData
	currentViewIndex int
	list             list.Model
	source           viewport.Model
	styles           Styles
	ready            bool
}

func newModel(data *ProfileData) model {
	m := model{
		profileData:      data,
		currentViewIndex: 0,
		list:             list.New(nil, list.NewDefaultDelegate(), 0, 0),
		source:           viewport.New(0, 0),
		styles:           defaultStyles(),
	}
	m.setActiveView()
	m.source.Style = m.styles.Source
	return m
}

func (m *model) setActiveView() {
	currentView := m.profileData.Views[m.currentViewIndex]
	m.list.Title = currentView.Name

	// --- THIS BLOCK IS NOW CORRECTED ---
	unit := "bytes" // Default
	if name := currentView.Name; len(name) > 0 {
		// Use the standard library's strings.Contains
		if strings.Contains(name, "nanoseconds") {
			unit = "nanoseconds"
		} else if strings.Contains(name, "count") || strings.Contains(name, "objects") {
			unit = "count"
		}
	}
	// --- END OF CORRECTION ---

	items := make([]list.Item, len(currentView.Functions))
	for i, f := range currentView.Functions {
		items[i] = listItem{
			prof:        f,
			valueString: fmt.Sprintf("Flat: %s", formatValue(f.FlatValue, unit)),
		}
	}
	m.list.SetItems(items)
	// Reset the list's viewport and cursor to the top
	m.list.ResetSelected()
	m.list.Paginator.Page = 0

	m.updateSourceView()
}

func (m model) Init() tea.Cmd {
	return nil
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		h, v := m.styles.Base.GetFrameSize()
		listWidth := int(float64(msg.Width-h) * 0.4)
		sourceWidth := msg.Width - h - listWidth
		paneHeight := msg.Height - v - 3

		m.list.SetSize(listWidth, paneHeight)
		m.styles.List = m.styles.List.Width(listWidth).Height(paneHeight)
		m.styles.Source = m.styles.Source.Width(sourceWidth).Height(paneHeight)
		m.source.Width = sourceWidth
		m.source.Height = paneHeight

		if !m.ready {
			m.ready = true
			m.updateSourceView()
		}
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "q":
			return m, tea.Quit
		case "t":
			// Cycle to the next profile view
			m.currentViewIndex = (m.currentViewIndex + 1) % len(m.profileData.Views)
			m.setActiveView()
			return m, nil
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
	selected, ok := m.list.SelectedItem().(listItem)
	if !ok {
		m.source.SetContent("No function selected.")
		return
	}
	content := getHighlightedSource(selected.prof.FileName, selected.prof.StartLine)
	m.source.SetContent(content)
	halfViewportHeight := m.source.Height / 2
	scrollPos := selected.prof.StartLine - halfViewportHeight
	if scrollPos < 0 {
		scrollPos = 0
	}
	m.source.SetYOffset(scrollPos)
}

func (m model) View() string {
	if !m.ready || m.profileData == nil {
		return "Initializing..."
	}

	statusText := m.styles.Status.Render(
		fmt.Sprintf("Total Functions: %d | Use ↑/↓ to navigate, t to change view, q to quit", len(m.list.Items())),
	)
	panes := lipgloss.JoinHorizontal(lipgloss.Top,
		m.styles.List.Render(m.list.View()),
		m.styles.Source.Render(m.source.View()),
	)
	return m.styles.Base.Render(lipgloss.JoinVertical(lipgloss.Left, panes, statusText))
}
