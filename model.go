// model.go
package main

import (
	"fmt"
	"sort"

	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type sortOrder int

const (
	byFlat sortOrder = iota
	byCum
	byName
)

func (s sortOrder) String() string {
	return []string{"Flat", "Cumulative", "Name"}[s]
}

type viewMode int

const (
	sourceView viewMode = iota
	graphView
)

// listItem now represents a FuncNode.
type listItem struct {
	node *FuncNode
	unit string
}

func (i listItem) Title() string { return i.node.Name }
func (i listItem) Description() string {
	return fmt.Sprintf("Flat: %s | Cum: %s",
		formatValue(i.node.FlatValue, i.unit),
		formatValue(i.node.CumValue, i.unit),
	)
}
func (i listItem) FilterValue() string { return i.node.Name }

type model struct {
	profileData      *ProfileData
	currentViewIndex int
	mode             viewMode
	sort             sortOrder

	// UI components
	mainList    list.Model
	source      viewport.Model
	callersList list.Model
	calleesList list.Model

	styles Styles
	ready  bool
}

func newModel(data *ProfileData) model {
	styles := defaultStyles()
	m := model{
		profileData:      data,
		currentViewIndex: 0,
		mode:             sourceView,
		sort:             byFlat,
		mainList:         list.New(nil, list.NewDefaultDelegate(), 0, 0),
		source:           viewport.New(0, 0),
		callersList:      list.New(nil, list.NewDefaultDelegate(), 0, 0),
		calleesList:      list.New(nil, list.NewDefaultDelegate(), 0, 0),
		styles:           styles,
	}
	m.source.Style = styles.Source
	m.callersList.Title = "Callers"
	m.calleesList.Title = "Callees"
	m.setActiveView()
	return m
}

// setActiveView now just sets the title and calls our new sorting/updating function.
func (m *model) setActiveView() {
	currentView := m.profileData.Views[m.currentViewIndex]
	m.mainList.Title = fmt.Sprintf("View: %s", currentView.Name)
	m.resortAndSetList()
}

func (m *model) resortAndSetList() {
	currentView := m.profileData.Views[m.currentViewIndex]
	nodes := make([]*FuncNode, 0, len(currentView.Nodes))
	for _, node := range currentView.Nodes {
		nodes = append(nodes, node)
	}

	// Apply the current sort order
	switch m.sort {
	case byFlat:
		sort.Slice(nodes, func(i, j int) bool { return nodes[i].FlatValue > nodes[j].FlatValue })
	case byCum:
		sort.Slice(nodes, func(i, j int) bool { return nodes[i].CumValue > nodes[j].CumValue })
	case byName:
		sort.Slice(nodes, func(i, j int) bool { return nodes[i].Name < nodes[j].Name })
	}

	items := make([]list.Item, len(nodes))
	for i, node := range nodes {
		items[i] = listItem{node: node, unit: currentView.Unit}
	}

	m.mainList.SetItems(items)
	m.updateChildPanes()
}

// updateChildPanes updates the right-hand side based on the current mode and selection.
func (m *model) updateChildPanes() {
	selected, ok := m.mainList.SelectedItem().(listItem)
	if !ok {
		m.source.SetContent("No function selected.")
		m.callersList.SetItems(nil)
		m.calleesList.SetItems(nil)
		return
	}

	// Update Source View
	content := getHighlightedSource(selected.node.FileName, selected.node.StartLine)
	m.source.SetContent(content)
	halfViewportHeight := m.source.Height / 2
	scrollPos := selected.node.StartLine - halfViewportHeight
	if scrollPos < 0 {
		scrollPos = 0
	}
	m.source.SetYOffset(scrollPos)

	// Update Graph View (Callers/Callees)
	m.updateGraphLists(selected.node)
}

// updateGraphLists populates the caller and callee lists.
func (m *model) updateGraphLists(node *FuncNode) {
	unit := m.profileData.Views[m.currentViewIndex].Unit

	// Populate Callers
	callerItems := make([]list.Item, 0, len(node.In))
	for callerNode := range node.In {
		callerItems = append(callerItems, listItem{node: callerNode, unit: unit})
	}
	sort.Slice(callerItems, func(i, j int) bool {
		return callerItems[i].FilterValue() < callerItems[j].FilterValue()
	})
	m.callersList.SetItems(callerItems)

	// Populate Callees
	calleeItems := make([]list.Item, 0, len(node.Out))
	for calleeNode := range node.Out {
		calleeItems = append(calleeItems, listItem{node: calleeNode, unit: unit})
	}
	sort.Slice(calleeItems, func(i, j int) bool {
		return calleeItems[i].FilterValue() < calleeItems[j].FilterValue()
	})
	m.calleesList.SetItems(calleeItems)
}

func (m model) Init() tea.Cmd { return nil }

// model.go

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	// If the list is filtering, we only want to pass keystrokes to it.
	// We don't want our other keybindings (t, c, q) to be active.
	if m.mainList.FilterState() == list.Filtering {
		// Pass the message to the list and return.
		var cmd tea.Cmd
		m.mainList, cmd = m.mainList.Update(msg)

		if len(m.mainList.VisibleItems()) == 1 {
			m.updateChildPanes()
		}

		return m, cmd
	}

	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		h, v := m.styles.Base.GetFrameSize()
		listWidth := int(float64(msg.Width-h) * 0.4)
		rightPaneWidth := msg.Width - h - listWidth
		paneHeight := msg.Height - v - 3

		m.mainList.SetSize(listWidth, paneHeight)
		m.styles.List = m.styles.List.Width(listWidth).Height(paneHeight)
		m.source.Width = rightPaneWidth
		m.source.Height = paneHeight
		m.styles.Source = m.styles.Source.Width(rightPaneWidth).Height(paneHeight)
		graphListHeight := paneHeight / 2
		m.callersList.SetSize(rightPaneWidth, graphListHeight)
		m.calleesList.SetSize(rightPaneWidth, paneHeight-graphListHeight)

		if !m.ready {
			m.ready = true
			m.updateChildPanes()
		}

	case tea.KeyMsg:
		if m.mainList.FilterState() != list.Filtering {
			switch msg.String() {
			case "ctrl+c", "q":
				return m, tea.Quit
			case "t":
				m.currentViewIndex = (m.currentViewIndex + 1) % len(m.profileData.Views)
				m.setActiveView()
				return m, nil
			case "c":
				if m.mode == sourceView {
					m.mode = graphView
				} else {
					m.mode = sourceView
				}
				return m, nil
			case "s": // <-- NEW: Handle sort key
				m.sort = (m.sort + 1) % 3 // Cycle through the 3 sort orders
				m.resortAndSetList()
				return m, nil
			}
		}
	}

	// This block handles updates for navigation and child panes.
	// It's placed after the main switch to allow the list to process
	// navigation keys that aren't captured above (like up/down arrows).
	beforeIndex := m.mainList.Index()
	m.mainList, _ = m.mainList.Update(msg)
	if beforeIndex != m.mainList.Index() {
		m.updateChildPanes()
	}

	// Also update child lists if in graph view
	if m.mode == graphView {
		m.callersList, _ = m.callersList.Update(msg)
		m.calleesList, _ = m.calleesList.Update(msg)
	} else {
		m.source, _ = m.source.Update(msg)
	}

	return m, tea.Batch(cmds...)
}

// View function now includes the sort order in the status bar.
func (m model) View() string {
	if !m.ready || m.profileData == nil {
		return "Initializing..."
	}

	var rightPane string
	if m.mode == sourceView {
		rightPane = m.styles.Source.Render(m.source.View())
	} else {
		rightPane = lipgloss.JoinVertical(lipgloss.Left, m.callersList.View(), m.calleesList.View())
	}

	statusText := m.styles.Status.Render(
		fmt.Sprintf("Sort: %s | ↑/↓ nav | t view | c mode | / filter | s sort | q quit", m.sort.String()),
	)

	panes := lipgloss.JoinHorizontal(lipgloss.Top, m.styles.List.Render(m.mainList.View()), rightPane)
	return m.styles.Base.Render(lipgloss.JoinVertical(lipgloss.Left, panes, statusText))
}
