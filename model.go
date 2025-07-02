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

// Predefined layouts the user can cycle through.
var layoutRatios = []float64{0.4, 0.6, 0.3} // 40/60, 60/40, 30/70

func (s sortOrder) String() string {
	return []string{"Self", "Total", "Name"}[s]
}

type viewMode int

const (
	sourceView viewMode = iota
	graphView
	flameGraphView
)

type model struct {
	profileData      *ProfileData
	currentViewIndex int
	mode             viewMode
	sort             sortOrder
	sourceInfo       string
	isDiffMode       bool
	showProjectOnly  bool

	// UI components
	mainList    list.Model
	source      viewport.Model
	callersList list.Model
	calleesList list.Model

	// Flamegraph zoom
	zoomedFlameRoot *FlameNode // Current zoom root (nil = full view)

	width       int
	height      int
	layoutIndex int

	styles   Styles
	ready    bool
	showHelp bool
	helpView viewport.Model
}

type listItem struct {
	node        *FuncNode
	unit        string
	viewName    string
	styles      *Styles
	TotalValue  int64
	edgeValue   int64
	contextNode *FuncNode
	isCaller    bool
}

func newModel(data *ProfileData, sourceInfo string) model {
	styles := defaultStyles()
	isDiff := strings.HasPrefix(sourceInfo, "Diff:")

	m := model{
		profileData:      data,
		currentViewIndex: 0,
		sourceInfo:       sourceInfo,
		isDiffMode:       isDiff,
		showProjectOnly:  false,
		mode:             sourceView,
		sort:             byFlat,
		layoutIndex:      0,
		helpView:         viewport.New(0, 0),
		showHelp:         false,
		mainList:         list.New(nil, list.NewDefaultDelegate(), 0, 0),
		callersList:      list.New(nil, list.NewDefaultDelegate(), 0, 0),
		calleesList:      list.New(nil, list.NewDefaultDelegate(), 0, 0),
		source:           viewport.New(0, 0),
		styles:           styles,
	}
	m.source.Style = styles.Source

	// Configure Callers list
	m.callersList.Title = "Callers"
	m.callersList.SetShowHelp(false)
	m.callersList.SetShowStatusBar(false)

	// Configure Callees list (FIXED)
	m.calleesList.Title = "Callees"
	m.calleesList.SetShowHelp(false)
	m.calleesList.SetShowStatusBar(false) // Corrected from m.callersList

	m.setActiveView()
	return m
}

func (i listItem) Title() string {
	if i.node.IsProjectCode {
		return i.styles.ProjectCode.Render("★ " + i.node.Name)
	}
	return i.node.Name
}

func (i listItem) Description() string {
	formatPercent := func(val int64, total int64) string {
		if total == 0 {
			return " (100.0%)"
		}
		if val == 0 {
			return ""
		}
		percent := (float64(val) / float64(total)) * 100
		if percent < 0.1 {
			return " (<0.1%)"
		}
		return fmt.Sprintf(" (%.1f%%)", percent)
	}
	isDiff := strings.HasPrefix(i.viewName, "Diff:")

	// Case 1: Caller/Callee context.
	if i.contextNode != nil {
		// Sub-case 1A: We are in a diff view. The edge value is a DELTA.
		if isDiff {
			edgeStr := formatDelta(i.edgeValue, i.unit, i.styles)
			if i.isCaller {
				// Example: "This function's call *to* the selection changed by +50ms"
				return fmt.Sprintf("This function's call to the selection changed by %s", edgeStr)
			} else {
				// Example: "This call *from* the selection changed by -1.2MiB"
				return fmt.Sprintf("This call from the selection changed by %s", edgeStr)
			}
		}

		// Sub-case 1B: Normal (non-diff) caller/callee view.
		edgeStr := formatValue(i.edgeValue, i.unit)
		if i.isCaller {
			percentOfCallersTotal := formatPercent(i.edgeValue, i.node.CumValue)
			return fmt.Sprintf(
				"this function called the one you selected, a call responsible for %s (%s of this function's total)",
				edgeStr, strings.TrimSpace(percentOfCallersTotal),
			)
		} else {
			percentOfSelectedsTotal := formatPercent(i.edgeValue, i.contextNode.CumValue)
			return fmt.Sprintf(
				"was called by the selected function, accounting for %s (%s of its total cost)",
				edgeStr, strings.TrimSpace(percentOfSelectedsTotal),
			)
		}
	}

	// Case 2: Diff mode
	if isDiff {
		flatStr := formatDelta(i.node.FlatDelta, i.unit, i.styles)
		cumStr := formatDelta(i.node.CumDelta, i.unit, i.styles)
		return fmt.Sprintf("own Δ: %s | total Δ: %s", flatStr, cumStr)
	}

	// Case 3: Main list descriptions - the final, refined version.
	ownVal := i.node.FlatValue
	totalVal := i.node.CumValue
	ownStr := formatValue(ownVal, i.unit)
	totalStr := formatValue(totalVal, i.unit)

	// Is 98% or more of the cost flat? If so, it's a "worker".
	isWorker := totalVal == 0 || (float64(ownVal)/float64(totalVal)) >= 0.98

	switch {
	case strings.Contains(i.viewName, "alloc_space"), strings.Contains(i.viewName, "alloc_objects"):
		verb := "allocated"
		noun := "of memory"
		if strings.Contains(i.viewName, "objects") {
			verb = "created"
			noun = "objects"
		}
		base := fmt.Sprintf("%s %s %s on its own", verb, ownStr, noun)
		if isWorker {
			return base
		}
		return fmt.Sprintf("%s; %s total including callees", base, totalStr)

	case strings.Contains(i.viewName, "inuse_space"), strings.Contains(i.viewName, "inuse_objects"):
		noun := "of memory"
		if strings.Contains(i.viewName, "objects") {
			noun = "objects"
		}
		base := fmt.Sprintf("accounted for %s %s on its own", ownStr, noun)
		if isWorker {
			return base
		}
		return fmt.Sprintf("%s; %s total including callees", base, totalStr)

	case strings.Contains(i.viewName, "cpu"), strings.Contains(i.viewName, "samples"):
		// Special case for clear "manager" functions
		if totalVal > 0 && (float64(ownVal)/float64(totalVal)) < 0.15 {
			return fmt.Sprintf("mainly waited for callees (%s), with only %s of its own work", totalStr, ownStr)
		}

		base := fmt.Sprintf("spent %s doing its own work", ownStr)
		if isWorker {
			return base
		}
		return fmt.Sprintf("%s; %s total including callees", base, totalStr)

	default: // Generic fallback
		return fmt.Sprintf("Flat: %s; Cum: %s", ownStr, totalStr)
	}
}

func (i listItem) FilterValue() string { return i.node.Name }

func (m *model) applyPaneSizes() {
	h, v := m.styles.Base.GetFrameSize()

	header := m.renderDiagnosticHeader()
	headerHeight := lipgloss.Height(header)

	statusHeight := 1

	paneHeight := m.height - v - headerHeight - statusHeight

	splitRatio := layoutRatios[m.layoutIndex]
	availableWidth := m.width - h
	listWidth := int(float64(availableWidth) * splitRatio)
	rightPaneWidth := availableWidth - listWidth

	m.mainList.SetSize(listWidth, paneHeight)
	m.styles.List = m.styles.List.Width(listWidth).Height(paneHeight)
	m.source.Width = rightPaneWidth
	m.source.Height = paneHeight
	m.styles.Source = m.styles.Source.Width(rightPaneWidth).Height(paneHeight)
	graphListHeight := paneHeight / 2
	m.callersList.SetSize(rightPaneWidth, graphListHeight)
	m.calleesList.SetSize(rightPaneWidth, paneHeight-graphListHeight)
	m.helpView.Width = m.width - h
	m.helpView.Height = paneHeight
}

func (m *model) setActiveView() {
	currentView := m.profileData.Views[m.currentViewIndex]
	title := fmt.Sprintf("View: %s", currentView.Name)
	if m.showProjectOnly {
		title += " (Project Only)"
	}
	m.mainList.Title = title
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

	items := make([]list.Item, 0, len(nodes))
	for _, node := range nodes {
		if m.showProjectOnly && !node.IsProjectCode {
			continue // Skip if we're in project-only mode and this node isn't project code.
		}
		items = append(items, listItem{
			node:       node,
			unit:       currentView.Unit,
			viewName:   currentView.Name,
			styles:     &m.styles,
			TotalValue: currentView.TotalValue,
		})
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
	viewName := currentView.Name

	// Populate Callers
	callerItems := make([]list.Item, 0, len(selectedNode.In))
	for callerNode, edgeVal := range selectedNode.In {
		callerItems = append(callerItems, listItem{
			node:        callerNode,
			unit:        unit,
			viewName:    viewName,
			styles:      &m.styles,
			TotalValue:  totalValue,
			edgeValue:   edgeVal,
			contextNode: selectedNode,
			isCaller:    true, // This is a caller
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
			viewName:    viewName,
			styles:      &m.styles,
			TotalValue:  totalValue,
			edgeValue:   edgeVal,
			contextNode: selectedNode,
			isCaller:    false, // This is a callee
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
		m.width = msg.Width
		m.height = msg.Height

		m.applyPaneSizes()

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
				if m.isDiffMode {
					return m, nil
				}
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
			case "r":
				m.layoutIndex = (m.layoutIndex + 1) % len(layoutRatios)
				m.applyPaneSizes()
				return m, nil
			case "p":
				m.showProjectOnly = !m.showProjectOnly
				m.setActiveView() // Re-render the view with the new filter state
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
		helpItems := []string{
			"F1 help",
			fmt.Sprintf("s sort (%s)", sortStr),
			"t view",
			"c mode",
			"p project",
		}
		if !m.isDiffMode {
			helpItems = append(helpItems, "f flame")
		}
		helpItems = append(helpItems, "r resize", "q quit")
		statusText = m.styles.Status.Render(strings.Join(helpItems, " | "))
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
