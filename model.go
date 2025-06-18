// model.go
package main

import (
	"fmt"
	"sort"
	"strings"
	"time"

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
	flameGraphView
)

// listItem now represents a FuncNode.
type listItem struct {
	node       *FuncNode
	unit       string
	styles     *Styles
	TotalValue int64
}

func (i listItem) Title() string { return i.node.Name }
func (i listItem) Description() string {
	if i.node.FlatDelta != 0 || i.node.CumDelta != 0 {
		// Diff mode - percentages are less meaningful here, so let's keep it clean.
		flatStr := formatDelta(i.node.FlatDelta, i.unit, i.styles)
		cumStr := formatDelta(i.node.CumDelta, i.unit, i.styles)
		return fmt.Sprintf("Flat: %s | Cum: %s", flatStr, cumStr)
	}

	var flatPercent, cumPercent float64
	if i.TotalValue > 0 {
		flatPercent = (float64(i.node.FlatValue) / float64(i.TotalValue)) * 100
		cumPercent = (float64(i.node.CumValue) / float64(i.TotalValue)) * 100
	}

	flatStr := formatValue(i.node.FlatValue, i.unit)
	cumStr := formatValue(i.node.CumValue, i.unit)

	valueAndPercentStr := fmt.Sprintf("%s (%.1f%%)", flatStr, flatPercent)
	cumAndPercentStr := fmt.Sprintf("%s (%.1f%%)", cumStr, cumPercent)

	return fmt.Sprintf("Flat: %s | Cum: %s", valueAndPercentStr, cumAndPercentStr)
}
func (i listItem) FilterValue() string { return i.node.Name }

type model struct {
	profileData      *ProfileData
	currentViewIndex int
	mode             viewMode
	sort             sortOrder
	sourceInfo       string

	// UI components
	mainList    list.Model
	source      viewport.Model
	callersList list.Model
	calleesList list.Model

	styles   Styles
	ready    bool
	showHelp bool
	helpView viewport.Model
}

func newModel(data *ProfileData, sourceInfo string) model {
	styles := defaultStyles()
	m := model{
		profileData:      data,
		currentViewIndex: 0,
		sourceInfo:       sourceInfo,
		mode:             sourceView,
		sort:             byFlat,
		helpView:         viewport.New(0, 0),
		showHelp:         false,
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
		// Pass the view's TotalValue into each item we create.
		items[i] = listItem{
			node:       node,
			unit:       currentView.Unit,
			styles:     &m.styles,
			TotalValue: currentView.TotalValue,
		}
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
	var cmd tea.Cmd
	var cmds []tea.Cmd
	if m.showHelp {
		// If help is showing, all we do is pass messages to its viewport for scrolling,
		// and check for keys to close it.
		if msg, ok := msg.(tea.KeyMsg); ok {
			switch msg.String() {
			case "h", "q", "esc":
				m.showHelp = false
			}
		}
		// Update the help viewport. This will handle scrolling.
		m.helpView, cmd = m.helpView.Update(msg)
		cmds = append(cmds, cmd)
		return m, tea.Batch(cmds...)
	}
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

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		h, v := m.styles.Base.GetFrameSize()

		header := m.renderDiagnosticHeader()
		headerHeight := lipgloss.Height(header)

		statusHeight := 1

		paneHeight := msg.Height - v - headerHeight - statusHeight

		listWidth := int(float64(msg.Width-h) * 0.4)
		rightPaneWidth := msg.Width - h - listWidth

		m.mainList.SetSize(listWidth, paneHeight)
		m.styles.List = m.styles.List.Width(listWidth).Height(paneHeight)
		m.source.Width = rightPaneWidth
		m.source.Height = paneHeight
		m.styles.Source = m.styles.Source.Width(rightPaneWidth).Height(paneHeight)
		graphListHeight := paneHeight / 2
		m.callersList.SetSize(rightPaneWidth, graphListHeight)
		m.calleesList.SetSize(rightPaneWidth, paneHeight-graphListHeight)

		m.helpView.Width = msg.Width - h
		m.helpView.Height = paneHeight

		if !m.ready {
			m.ready = true
			m.updateChildPanes()
		}

	case tea.KeyMsg:
		if m.mainList.FilterState() != list.Filtering {
			switch msg.String() {
			case "h":
				// 1. Get the specific help for the current view.
				viewExplanation := getExplanationForView(m.mainList.Title)

				// 2. Get our new general help topics.
				flatCumExplanation := explainerMap["flat_vs_cum"]
				flameGraphExplanation := explainerMap["flamegraph"]

				// 3. Build a comprehensive help text.
				var helpBuilder strings.Builder
				helpBuilder.WriteString(fmt.Sprintf("# %s\n\n%s\n\n", viewExplanation.Title, viewExplanation.Description))
				helpBuilder.WriteString("---\n\n")
				helpBuilder.WriteString(fmt.Sprintf("# %s\n\n%s\n\n", flatCumExplanation.Title, flatCumExplanation.Description))
				helpBuilder.WriteString("---\n\n")
				helpBuilder.WriteString(fmt.Sprintf("# %s\n\n%s", flameGraphExplanation.Title, flameGraphExplanation.Description))

				m.helpView.SetContent(helpBuilder.String())
				m.helpView.GotoTop()
				m.showHelp = true
				return m, nil
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
			case "s":
				m.sort = (m.sort + 1) % 3 // Cycle through the 3 sort orders
				m.resortAndSetList()
				return m, nil
			case "f":
				if m.mode == flameGraphView {
					m.mode = sourceView // Toggle back to source view
				} else {
					m.mode = flameGraphView
				}
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
	} else if m.mode == sourceView {
		m.source, _ = m.source.Update(msg)
	}

	return m, tea.Batch(cmds...)
}

func (m model) View() string {
	if m.showHelp {
		return m.styles.Base.Render(m.helpView.View())
	}

	if !m.ready {
		return "Initializing..."
	}

	// 1. Render the header
	header := m.renderDiagnosticHeader()

	// 2. Render the middle panes
	var rightPane string
	if m.mode == sourceView {
		rightPane = m.styles.Source.Render(m.source.View())
	} else if m.mode == graphView {
		rightPane = lipgloss.JoinVertical(lipgloss.Left, m.callersList.View(), m.calleesList.View())
	} else {
		currentViewIndex := m.currentViewIndex
		flameRoot := BuildFlameGraph(m.profileData.RawPprof, currentViewIndex)
		rightPaneWidth := m.source.Width
		rightPane = RenderFlameGraph(flameRoot, rightPaneWidth)
	}
	panes := lipgloss.JoinHorizontal(lipgloss.Top, m.styles.List.Render(m.mainList.View()), rightPane)

	statusText := m.styles.Status.Render(
		"h help | s sort | t view | c mode | f flame | q quit",
	)

	// 4. Join them all vertically
	return m.styles.Base.Render(lipgloss.JoinVertical(lipgloss.Left, header, panes, statusText))
}

func formatDelta(value int64, unit string, s *Styles) string {
	formattedVal := formatValue(abs(value), unit)
	if value > 0 {
		return s.DiffPositive.Render(fmt.Sprintf("+%s", formattedVal))
	}
	if value < 0 {
		return s.DiffNegative.Render(fmt.Sprintf("-%s", formattedVal))
	}
	return formattedVal
}

func abs(x int64) int64 {
	if x < 0 {
		return -x
	}
	return x
}

func (m model) renderDiagnosticHeader() string {
	if m.profileData == nil || len(m.profileData.Views) == 0 {
		return ""
	}

	currentView := m.profileData.Views[m.currentViewIndex]
	var diagnosticText string

	// No change to Diff or CPU logic
	if strings.HasPrefix(currentView.Name, "Diff:") {
		diagnosticText = "💡 Comparing two profiles. Green (+) means more time/memory was used in the second profile. Red (-) means less."
	} else if strings.Contains(currentView.Name, "cpu") || strings.Contains(currentView.Name, "samples") {
		var cpuTimeView *ProfileView
		for _, v := range m.profileData.Views {
			if strings.Contains(v.Name, "cpu") && strings.Contains(v.Name, "nanoseconds") {
				cpuTimeView = v
				break
			}
		}
		if cpuTimeView == nil {
			cpuTimeView = currentView
		}
		profileDuration := m.profileData.DurationNanos
		sampleDuration := cpuTimeView.TotalValue
		if profileDuration > 0 && cpuTimeView.Unit == "nanoseconds" {
			busyPercent := (float64(sampleDuration) / float64(profileDuration)) * 100
			profDurStr := time.Duration(profileDuration).Round(time.Millisecond).String()
			sampDurStr := time.Duration(sampleDuration).Round(time.Millisecond).String()
			summary := fmt.Sprintf("Profiled for %s. Your program was busy for %s (%.1f%%).", profDurStr, sampDurStr, busyPercent)
			var hint string
			if busyPercent < 5.0 {
				hint = "This program is likely I/O-bound or blocked. The CPU profile may not show the real bottleneck."
			} else if busyPercent > 95.0 {
				hint = "This program is CPU-bound. The functions below are the main contributors to CPU usage."
			} else {
				hint = "This program has a mix of CPU and I/O/blocking work."
			}
			diagnosticText = summary + "\n💡 " + hint
		}
	} else if strings.Contains(currentView.Name, "inuse") {
		diagnosticText = "💡 Think 'Water Level'. This shows memory held *right now*. Use this view to find memory leaks."
	} else if strings.Contains(currentView.Name, "alloc") {
		diagnosticText = "💡 Think 'Total Water Poured'. This shows all memory allocated over time. Use this to find code causing GC pressure."
	}

	if diagnosticText == "" {
		// If there's no special hint, just show the source info plainly without a clunky box.
		return m.styles.Status.Render(m.sourceInfo)
	}

	var content strings.Builder
	content.WriteString(m.sourceInfo)
	content.WriteString("\n" + diagnosticText)

	return m.styles.Header.Render(content.String())
}
