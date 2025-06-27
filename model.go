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
	return []string{"Flat", "Cum", "Name"}[s]
}

type viewMode int

const (
	sourceView viewMode = iota
	graphView
	flameGraphView
)

type listItem struct {
	node        *FuncNode
	unit        string
	styles      *Styles
	TotalValue  int64
	edgeValue   int64     // The value of the specific edge (e.g., from caller to selected)
	contextNode *FuncNode // The node this item is relative to (the selected node in the main list)
}

func (i listItem) Title() string { return i.node.Name }
func (i listItem) Description() string {
	// Case 1: Caller/Callee list item (has context)
	if i.contextNode != nil {
		var edgePercentOfCum float64
		// Calculate what percentage of the context node's cumulative value this edge represents
		if i.contextNode.CumValue > 0 {
			edgePercentOfCum = (float64(i.edgeValue) / float64(i.contextNode.CumValue)) * 100
		}
		edgeStr := formatValue(i.edgeValue, i.unit)

		// Also show the node's own flat/cum values for reference
		flatStr := formatValue(i.node.FlatValue, i.unit)
		cumStr := formatValue(i.node.CumValue, i.unit)

		return fmt.Sprintf("Edge: %s (%.1f%% of caller's Cum) | Flat: %s | Cum: %s", edgeStr, edgePercentOfCum, flatStr, cumStr)
	}

	// Case 2: Diff mode main list item
	if i.node.FlatDelta != 0 || i.node.CumDelta != 0 {
		flatStr := formatDelta(i.node.FlatDelta, i.unit, i.styles)
		cumStr := formatDelta(i.node.CumDelta, i.unit, i.styles)
		return fmt.Sprintf("Flat: %s | Cum: %s", flatStr, cumStr)
	}

	// Case 3: Normal main list item
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
	isDiffMode       bool

	// UI components
	mainList    list.Model
	source      viewport.Model
	callersList list.Model
	calleesList list.Model

	// Flamegraph zoom
	zoomedFlameRoot *FlameNode // Current zoom root (nil = full view)

	styles   Styles
	ready    bool
	showHelp bool
	helpView viewport.Model
}

func newModel(data *ProfileData, sourceInfo string) model {
	styles := defaultStyles()
	isDiff := strings.HasPrefix(sourceInfo, "Diff:")
	m := model{
		profileData:      data,
		currentViewIndex: 0,
		sourceInfo:       sourceInfo,
		isDiffMode:       isDiff,
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

	switch m.sort {
	case byFlat:
		if m.isDiffMode {
			sort.Slice(nodes, func(i, j int) bool { return abs(nodes[i].FlatDelta) > abs(nodes[j].FlatDelta) })
		} else {
			sort.Slice(nodes, func(i, j int) bool { return nodes[i].FlatValue > nodes[j].FlatValue })
		}
	case byCum:
		if m.isDiffMode {
			sort.Slice(nodes, func(i, j int) bool { return abs(nodes[i].CumDelta) > abs(nodes[j].CumDelta) })
		} else {
			sort.Slice(nodes, func(i, j int) bool { return nodes[i].CumValue > nodes[j].CumValue })
		}
	case byName:
		sort.Slice(nodes, func(i, j int) bool { return nodes[i].Name < nodes[j].Name })
	}

	items := make([]list.Item, len(nodes))
	for i, node := range nodes {
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

func (m model) currentSortString() string {
	baseSort := m.sort.String()
	if m.isDiffMode && (m.sort == byFlat || m.sort == byCum) {
		return baseSort + " Δ" // Add delta symbol in diff mode
	}
	return baseSort
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
func (m *model) updateGraphLists(selectedNode *FuncNode) {
	currentView := m.profileData.Views[m.currentViewIndex]
	unit := currentView.Unit
	totalValue := currentView.TotalValue // The total for the whole view

	// Populate Callers
	callerItems := make([]list.Item, 0, len(selectedNode.In))
	for callerNode, edgeVal := range selectedNode.In {
		callerItems = append(callerItems, listItem{
			node:        callerNode,
			unit:        unit,
			styles:      &m.styles,
			TotalValue:  totalValue,   // Pass total value
			edgeValue:   edgeVal,      // This is the weight of the edge from the caller
			contextNode: selectedNode, // The node we're viewing callers of
		})
	}
	// Sort callers by the edge weight (most impactful callers first)
	sort.Slice(callerItems, func(i, j int) bool {
		return callerItems[i].(listItem).edgeValue > callerItems[j].(listItem).edgeValue
	})
	m.callersList.SetItems(callerItems)

	// Populate Callees
	calleeItems := make([]list.Item, 0, len(selectedNode.Out))
	for calleeNode, edgeVal := range selectedNode.Out {
		calleeItems = append(calleeItems, listItem{
			node:        calleeNode,
			unit:        unit,
			styles:      &m.styles,
			TotalValue:  totalValue,
			edgeValue:   edgeVal,      // This is the weight of the edge to the callee
			contextNode: selectedNode, // The node we're viewing callees of
		})
	}
	// Sort callees by the edge weight (most expensive calls first)
	sort.Slice(calleeItems, func(i, j int) bool {
		return calleeItems[i].(listItem).edgeValue > calleeItems[j].(listItem).edgeValue
	})
	m.calleesList.SetItems(calleeItems)
}

func (m model) Init() tea.Cmd { return nil }

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	var cmds []tea.Cmd
	if m.showHelp {
		if msg, ok := msg.(tea.KeyMsg); ok {
			switch msg.String() {
			case "f1", "q", "esc":
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
			case "f1":
				viewExplanation := getExplanationForView(m.mainList.Title)

				flatCumExplanation := explainerMap["flat_vs_cum"]
				flameGraphExplanation := explainerMap["flamegraph"]

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
					m.mode = sourceView     // Toggle back to source view
					m.zoomedFlameRoot = nil // Reset zoom when exiting flamegraph
				} else {
					m.mode = flameGraphView
				}
				return m, nil
			case "enter":
				if m.mode == flameGraphView {
					// Zoom into the selected function from the main list
					selected, ok := m.mainList.SelectedItem().(listItem)
					if ok {
						// Find this function in the flamegraph
						currentViewIndex := m.currentViewIndex
						flameRoot := BuildFlameGraph(m.profileData.RawPprof, currentViewIndex)
						if targetNode := findNodeByName(flameRoot, selected.node.Name); targetNode != nil {
							m.zoomedFlameRoot = targetNode
						}
					}
					return m, nil
				}
			case "esc":
				if m.mode == flameGraphView && m.zoomedFlameRoot != nil {
					// Zoom out to full view
					m.zoomedFlameRoot = nil
					return m, nil
				}
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

		// Use zoomed root if we're zoomed in
		displayRoot := flameRoot
		if m.zoomedFlameRoot != nil {
			displayRoot = m.zoomedFlameRoot
		}

		rightPaneWidth := m.source.Width
		rightPane = RenderFlameGraph(displayRoot, rightPaneWidth)
	}
	panes := lipgloss.JoinHorizontal(lipgloss.Top, m.styles.List.Render(m.mainList.View()), rightPane)

	var statusText string
	if m.mode == flameGraphView {
		if m.zoomedFlameRoot != nil {
			statusText = m.styles.Status.Render(
				"F1 help | esc zoom out | f exit flame | t view | q quit",
			)
		} else {
			statusText = m.styles.Status.Render(
				"F1 help | enter zoom in | f exit flame | t view | q quit",
			)
		}
	} else {
		sortStr := m.currentSortString()
		statusText = m.styles.Status.Render(
			fmt.Sprintf("F1 help | s sort (%s) | t view | c mode | f flame | q quit", sortStr),
		)
	}

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
