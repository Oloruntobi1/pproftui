// model.go
package main

import (
	"fmt"
	"math"
	"slices"
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

// pane tracks which UI pane is currently focused, used for keyboard navigation.
type pane int

const (
	listPane pane = iota
	sourceCodePane
	flameGraphPane
)

type tickMsg time.Time

type profileUpdateMsg struct {
	data *ProfileData
}

type profileUpdateErr struct {
	err error
}

func (e profileUpdateErr) Error() string { return e.err.Error() }

type model struct {
	// Core Data
	profileData      *ProfileData
	currentViewIndex int
	mode             viewMode
	sort             sortOrder
	sourceInfo       string
	isDiffMode       bool
	showProjectOnly  bool

	// Live Mode State
	isLiveMode      bool
	isPaused        bool
	liveURL         string
	refreshInterval time.Duration
	modulePath      string
	lastError       error

	// UI components
	mainList    list.Model
	source      viewport.Model
	callersList list.Model
	calleesList list.Model

	// Flamegraph state
	flameGraphRoot     *FlameNode
	flameGraphFocus    *FlameNode
	flameGraphSelected *FlameNode // The user-selected node in the flame graph for keyboard nav
	flameGraphHover    *FlameNode
	flameGraphLayout   *[]FlameNodeRenderInfo
	paneFocus          pane // Tracks which pane (list or flamegraph) has focus.

	// General State
	width       int
	height      int
	layoutIndex int
	styles      Styles
	ready       bool
	showHelp    bool
	helpView    viewport.Model
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
		profileData:        data,
		currentViewIndex:   0,
		sourceInfo:         sourceInfo,
		isDiffMode:         isDiff,
		showProjectOnly:    false,
		mode:               sourceView,
		sort:               byFlat,
		layoutIndex:        0,
		helpView:           viewport.New(0, 0),
		showHelp:           false,
		mainList:           list.New(nil, list.NewDefaultDelegate(), 0, 0),
		callersList:        list.New(nil, list.NewDefaultDelegate(), 0, 0),
		calleesList:        list.New(nil, list.NewDefaultDelegate(), 0, 0),
		source:             viewport.New(0, 0),
		styles:             styles,
		flameGraphLayout:   &[]FlameNodeRenderInfo{},
		isPaused:           false, // Default to not paused
		paneFocus:          listPane,
		flameGraphSelected: nil,
	}
	m.source.Style = styles.Source

	m.mainList.SetShowHelp(false)

	// Configure lists
	m.callersList.Title = "Callers"
	m.callersList.SetShowHelp(false)
	m.callersList.SetShowStatusBar(false)
	m.calleesList.Title = "Callees"
	m.calleesList.SetShowHelp(false)
	m.calleesList.SetShowStatusBar(false)

	// If data is provided initially (static mode), set the active view.
	if data != nil {
		m.setActiveView()
	}

	return m
}

func (i listItem) Title() string {
	if i.node.IsProjectCode {
		return i.styles.ProjectCode.Render("★ " + i.node.Name)
	}
	return i.node.Name
}

func (i listItem) Description() string {
	formatPercent := func(val, total int64) string {
		if total == 0 {
			return "100.0%"
		}
		if val == 0 {
			return ""
		}
		percent := (float64(val) / float64(total)) * 100
		if percent < 0.1 {
			return "<0.1%"
		}
		return fmt.Sprintf("%.1f%%", percent)
	}

	isDiff := strings.HasPrefix(i.viewName, "Diff:")

	// Case 1: Caller/Callee context.
	if i.contextNode != nil {
		// Special handling for recursive calls, where the function appears in its own
		// caller/callee list.
		if i.node == i.contextNode {
			edgeStr := formatValue(i.edgeValue, i.unit)
			percent := formatPercent(i.edgeValue, i.node.CumValue)
			// This single, clear description works for both the Callers and Callees panes.
			return fmt.Sprintf("This function is recursive; its self-calls account for %s (%s of the total)", edgeStr, percent)
		}

		// Sub-case 1A: Diff view (original logic is preserved)
		if isDiff {
			edgeStr := formatDelta(i.edgeValue, i.unit, i.styles)
			if i.isCaller {
				return fmt.Sprintf("this function’s call to the selected one changed by %s", edgeStr)
			}
			return fmt.Sprintf("was called by the selected function, and that call changed by %s", edgeStr)
		}

		// Sub-case 1B: Normal (non-recursive) caller/callee view
		edgeStr := formatValue(i.edgeValue, i.unit)
		if i.isCaller {
			percent := formatPercent(i.edgeValue, i.node.CumValue)
			return fmt.Sprintf("called the selected function; this call accounts for %s (%s of this function’s total)", edgeStr, percent)
		}
		percent := formatPercent(i.edgeValue, i.contextNode.CumValue)
		return fmt.Sprintf("was called by the selected function, which triggered %s (%s of its total)", edgeStr, percent)
	}

	// Case 2: Diff mode
	if isDiff {
		flatStr := formatDelta(i.node.FlatDelta, i.unit, i.styles)
		cumStr := formatDelta(i.node.CumDelta, i.unit, i.styles)
		return fmt.Sprintf("own Δ: %s | total Δ: %s", flatStr, cumStr)
	}

	// Case 3: Main list descriptions
	ownVal := i.node.FlatValue
	totalVal := i.node.CumValue
	ownStr := formatValue(ownVal, i.unit)
	totalStr := formatValue(totalVal, i.unit)

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
		base := fmt.Sprintf("held %s %s on its own", ownStr, noun)
		if isWorker {
			return base
		}
		return fmt.Sprintf("%s; %s total including callees", base, totalStr)

	case strings.Contains(i.viewName, "cpu"), strings.Contains(i.viewName, "samples"):
		// Check for recursion. A function is recursive if it's in its own 'Out' map.
		_, isRecursive := i.node.Out[i.node]

		if isRecursive && !isWorker {
			// Use special phrasing for recursive functions.
			return fmt.Sprintf("is recursive, taking %s total; the top-level call took %s", totalStr, ownStr)
		}

		// Original logic for non-recursive functions.
		if totalVal > 0 && (float64(ownVal)/float64(totalVal)) < 0.15 {
			return fmt.Sprintf("mostly delegated work (%s total); did %s itself", totalStr, ownStr)
		}
		base := fmt.Sprintf("spent %s doing its own work", ownStr)
		if isWorker {
			return base
		}
		return fmt.Sprintf("%s; %s total including callees", base, totalStr)

	default: // fallback
		base := fmt.Sprintf("used %s on its own", ownStr)
		if isWorker {
			return base
		}
		return fmt.Sprintf("%s; %s total including callees", base, totalStr)
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
	if m.profileData == nil || len(m.profileData.Views) == 0 {
		return
	}
	// Make sure view index is valid
	if m.currentViewIndex >= len(m.profileData.Views) {
		m.currentViewIndex = 0
	}
	currentView := m.profileData.Views[m.currentViewIndex]
	title := fmt.Sprintf("View: %s", currentView.Name)
	if m.showProjectOnly {
		title += " (Project Only)"
	}
	m.mainList.Title = title
	// Invalidate flamegraph cache when view changes
	m.flameGraphRoot = nil
	m.flameGraphFocus = nil
	m.flameGraphHover = nil
	m.flameGraphSelected = nil
	*m.flameGraphLayout = nil
	m.resortAndSetList()
}

func (m *model) resortAndSetList() {
	if m.profileData == nil || len(m.profileData.Views) == 0 {
		return
	}
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

func (m model) Init() tea.Cmd {
	if m.isLiveMode {
		// For live mode, we start with an initial fetch and then start the ticker.
		return tea.Batch(
			fetchProfileCmd(m.liveURL, m.modulePath),
			tickerCmd(m.refreshInterval),
		)
	}
	return nil // No initial command for static mode
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	var cmds []tea.Cmd
	if m.showHelp {
		if msg, ok := msg.(tea.KeyMsg); ok {
			switch msg.String() {
			case "f1", "?", "q", "esc":
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
			// In static mode, this updates child panes. In live mode, we wait for data.
			if !m.isLiveMode {
				m.updateChildPanes()
			}
		}
	case tickMsg:
		if m.isLiveMode && !m.isPaused {
			cmds = append(cmds, fetchProfileCmd(m.liveURL, m.modulePath))
		}
		// Always return the ticker command to keep it going even if paused
		cmds = append(cmds, tickerCmd(m.refreshInterval))
		return m, tea.Batch(cmds...)

	case profileUpdateMsg:
		m.lastError = nil // Clear any previous error

		// Remember what function is currently selected to preserve state
		var selectedFuncName string
		if selected, ok := m.mainList.SelectedItem().(listItem); ok {
			selectedFuncName = selected.node.Name
		}

		m.profileData = msg.data

		// If this is the first data load, set up the view
		if m.mainList.Items() == nil {
			m.setActiveView()
		} else { // Otherwise, just refresh the list content
			m.resortAndSetList()
		}

		// Restore selection if possible
		if selectedFuncName != "" {
			for i, item := range m.mainList.Items() {
				if li, ok := item.(listItem); ok && li.node.Name == selectedFuncName {
					m.mainList.Select(i)
					break
				}
			}
		}

		// Refresh all dependent panes
		m.updateChildPanes()
		if m.mode == flameGraphView {
			m.rebuildFlameGraph()
		}

		return m, nil

	case profileUpdateErr:
		m.lastError = msg.err
		return m, nil

	case tea.MouseMsg:
		if msg.Action == tea.MouseActionMotion && m.mode == flameGraphView {
			// Calculate the starting position of the right pane.
			leftPaneRenderedWidth := lipgloss.Width(m.styles.List.Render(m.mainList.View()))
			headerHeight := lipgloss.Height(m.renderDiagnosticHeader())

			rpStyle := m.styles.Source
			rpTopPadding, _, _, rpLeftPadding := rpStyle.GetPadding()

			calculatedOriginY := headerHeight + rpStyle.GetBorderTopSize() + rpTopPadding

			// The true origin is the calculated one minus the observed offset.
			contentOriginY := calculatedOriginY - 2

			contentOriginX := leftPaneRenderedWidth + rpStyle.GetBorderLeftSize() + rpLeftPadding

			// Get mouse coordinates relative to the flamegraph's content area.
			relativeX := msg.X - contentOriginX
			relativeY := msg.Y - contentOriginY

			// Hit detection.
			found := false
			if m.flameGraphLayout != nil {
				for _, info := range *m.flameGraphLayout {
					// Check if the cursor is within the bounds of a rendered node.
					if relativeY == info.Y && relativeX >= info.X && relativeX < info.X+info.Width {
						m.flameGraphHover = info.Node
						found = true
						break
					}
				}
			}
			if !found {
				m.flameGraphHover = nil
			}
		}

	case tea.KeyMsg:
		// Handle keys differently if flame graph pane has focus.
		if m.mode == flameGraphView && m.paneFocus == flameGraphPane {
			if m.flameGraphSelected == nil {
				// If nothing is selected (e.g., first focus), select the focus/root node.
				m.flameGraphSelected = m.flameGraphFocus
				m.syncListToFlameGraphSelection()
				return m, nil
			}

			switch key := msg.String(); key {
			case "tab":
				m.paneFocus = listPane // Switch focus back to the list
				m.flameGraphSelected = nil
				return m, nil
			case "up", "k", "down", "j", "left", "h", "right", "l":
				m.navigateFlameGraph(key)
				return m, nil
			}
		}

		if m.mainList.FilterState() != list.Filtering {
			switch msg.String() {
			case " ": // Spacebar to pause/resume
				if m.isLiveMode {
					m.isPaused = !m.isPaused
					m.lastError = nil // Clear error on resume/pause
				}
				return m, nil
			case "f1", "?":
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

			case "tab": // Handle tab for focus switching
				if m.mode == sourceView {
					switch m.paneFocus {
					case sourceCodePane:
						m.paneFocus = listPane
					case listPane:
						m.paneFocus = sourceCodePane
					}
					return m, nil
				}

				if m.mode == flameGraphView {
					// This will only be reached if paneFocus is listPane
					m.paneFocus = flameGraphPane
					// Set initial selection to the node that corresponds to the list item
					selected, ok := m.mainList.SelectedItem().(listItem)
					if ok {
						m.flameGraphSelected = findNodeByName(m.flameGraphRoot, selected.node.Name)
					} else {
						// Fallback to the focus node if list has no selection
						m.flameGraphSelected = m.flameGraphFocus
					}
					return m, nil
				}

			case "t":
				if m.profileData != nil && len(m.profileData.Views) > 0 {
					m.currentViewIndex = (m.currentViewIndex + 1) % len(m.profileData.Views)
					m.setActiveView()
					if m.mode == flameGraphView {
						m.rebuildFlameGraph()
					}
				}
				return m, nil
			case "c":
				if m.mode == sourceView {
					m.mode = graphView
				} else {
					m.mode = sourceView
				}
				m.paneFocus = listPane // Reset focus on mode change
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
					m.mode = sourceView // Toggle back
					m.flameGraphRoot = nil
					m.flameGraphFocus = nil
					m.flameGraphSelected = nil
					m.paneFocus = listPane // Reset focus on leaving flame view
				} else {
					m.mode = flameGraphView
					m.rebuildFlameGraph()
				}
				return m, nil
			case "enter":
				if m.mode == flameGraphView {
					var nodeToFocus *FlameNode
					if m.paneFocus == flameGraphPane && m.flameGraphSelected != nil {
						// If focus is on the graph, 'enter' zooms into the keyboard-selected node
						nodeToFocus = m.flameGraphSelected
					} else {
						// Otherwise, it zooms into the list-selected node (original behavior)
						selected, ok := m.mainList.SelectedItem().(listItem)
						if ok && m.flameGraphRoot != nil {
							nodeToFocus = findNodeByName(m.flameGraphRoot, selected.node.Name)
						}
					}

					if nodeToFocus != nil {
						m.flameGraphFocus = nodeToFocus
						// After zooming, the new focus becomes the selected node
						m.flameGraphSelected = m.flameGraphFocus
						m.syncListToFlameGraphSelection()
					}
					return m, nil
				}
			case "esc":
				if m.mode == flameGraphView && m.flameGraphFocus != m.flameGraphRoot && m.flameGraphFocus != nil {
					// Zoom out to parent if zoomed in, otherwise go to root
					if m.flameGraphFocus.Parent != nil {
						m.flameGraphFocus = m.flameGraphFocus.Parent
					} else {
						m.flameGraphFocus = m.flameGraphRoot
					}
					// After zooming out, the new focus becomes the selected node
					m.flameGraphSelected = m.flameGraphFocus
					m.syncListToFlameGraphSelection()
					return m, nil
				}
			case "r":
				m.layoutIndex = (m.layoutIndex + 1) % len(layoutRatios)
				m.applyPaneSizes()
				return m, nil
			case "p":
				m.showProjectOnly = !m.showProjectOnly
				m.setActiveView() // This invalidates the old list and flamegraph
				if m.mode == flameGraphView {
					m.rebuildFlameGraph()
				}
				return m, nil
			}
		}
	}

	if m.mode == sourceView && m.paneFocus == sourceCodePane {
		m.source, _ = m.source.Update(msg)
		return m, tea.Batch(cmds...)
	}

	// This block handles updates for navigation and child panes.
	beforeIndex := m.mainList.Index()
	m.mainList, _ = m.mainList.Update(msg)
	if beforeIndex != m.mainList.Index() {
		m.updateChildPanes()
		// When list selection changes, update the graph selection if graph is not focused
		if m.mode == flameGraphView && m.paneFocus == listPane {
			m.flameGraphSelected = nil
		}
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

// findNodeRenderInfo finds the layout information for a given flame graph node.
func (m *model) findNodeRenderInfo(targetNode *FlameNode) (FlameNodeRenderInfo, bool) {
	if targetNode == nil || m.flameGraphLayout == nil {
		return FlameNodeRenderInfo{}, false
	}
	for _, info := range *m.flameGraphLayout {
		if info.Node == targetNode {
			return info, true
		}
	}
	return FlameNodeRenderInfo{}, false
}

// navigateFlameGraph handles the spatial navigation within the flame graph view.
func (m *model) navigateFlameGraph(direction string) {
	currentInfo, ok := m.findNodeRenderInfo(m.flameGraphSelected)
	if !ok {
		return // Cannot navigate if current selection isn't rendered
	}

	var nextNode *FlameNode

	switch direction {
	case "up", "k":
		// Navigate to parent, but not above the current focus point
		if m.flameGraphSelected.Parent != nil && m.flameGraphSelected != m.flameGraphFocus {
			nextNode = m.flameGraphSelected.Parent
		}

	case "down", "j":
		targetY := currentInfo.Y + 1
		centerX := currentInfo.X + currentInfo.Width/2
		var bestMatch *FlameNodeRenderInfo
		minDist := math.MaxInt32

		// Find the node on the row below that is closest to the center of the current node.
		for i := range *m.flameGraphLayout {
			candidateInfo := &(*m.flameGraphLayout)[i]
			if candidateInfo.Y == targetY {
				// Check if the center of the current node falls within the candidate's bounds.
				if centerX >= candidateInfo.X && centerX < candidateInfo.X+candidateInfo.Width {
					bestMatch = candidateInfo
					break // Perfect match found.
				}
				// If not a direct hit, find the closest one as a fallback.
				dist := min(abs(int64(centerX-candidateInfo.X)), abs(int64(centerX-(candidateInfo.X+candidateInfo.Width-1))))
				if dist < int64(minDist) {
					minDist = int(dist)
					bestMatch = candidateInfo
				}
			}
		}
		if bestMatch != nil {
			nextNode = bestMatch.Node
		}

	case "left", "h":
		targetY := currentInfo.Y
		var bestMatch *FlameNodeRenderInfo
		max_x := -1

		// Find the node on the same row, to the left, with the largest X coord (closest).
		for i := range *m.flameGraphLayout {
			candidateInfo := &(*m.flameGraphLayout)[i]
			if candidateInfo.Y == targetY && candidateInfo.X < currentInfo.X {
				if candidateInfo.X > max_x {
					max_x = candidateInfo.X
					bestMatch = candidateInfo
				}
			}
		}
		if bestMatch != nil {
			nextNode = bestMatch.Node
		}

	case "right", "l":
		targetY := currentInfo.Y
		var bestMatch *FlameNodeRenderInfo
		min_x := math.MaxInt32

		// Find the node on the same row, to the right, with the smallest X coord (closest).
		for i := range *m.flameGraphLayout {
			candidateInfo := &(*m.flameGraphLayout)[i]
			if candidateInfo.Y == targetY && candidateInfo.X > currentInfo.X {
				if candidateInfo.X < min_x {
					min_x = candidateInfo.X
					bestMatch = candidateInfo
				}
			}
		}
		if bestMatch != nil {
			nextNode = bestMatch.Node
		}
	}

	if nextNode != nil {
		m.flameGraphSelected = nextNode
		m.syncListToFlameGraphSelection()
	}
}

func (m *model) rebuildFlameGraph() {
	currentView := m.profileData.Views[m.currentViewIndex]
	m.flameGraphRoot = BuildFlameGraph(m.profileData.RawPprof, m.currentViewIndex, currentView.Unit)
	// Reset focus to the root of the new graph
	m.flameGraphFocus = m.flameGraphRoot
	// If the graph pane has focus, reset selection to the new root as well
	if m.paneFocus == flameGraphPane {
		m.flameGraphSelected = m.flameGraphRoot
	}
}

// syncListToFlameGraphSelection finds the item in the mainList that corresponds
// to the currently selected flame graph node and selects it.
func (m *model) syncListToFlameGraphSelection() {
	if m.flameGraphSelected == nil {
		return
	}
	for i, item := range m.mainList.Items() {
		if li, ok := item.(listItem); ok && li.node.Name == m.flameGraphSelected.Name {
			m.mainList.Select(i)
			break
		}
	}
}

func (m model) View() string {
	if m.showHelp {
		return m.styles.Base.Render(m.helpView.View())
	}

	if !m.ready {
		return "Initializing..."
	}

	header := m.renderDiagnosticHeader()
	var rightPane string

	// Define styles for panes, to be modified based on focus
	listStyle := m.styles.List
	sourceStyle := m.styles.Source
	activeBorderColor := lipgloss.Color("82") // A bright cyan for focus

	if m.mode == flameGraphView {
		if m.paneFocus == listPane {
			listStyle = listStyle.BorderForeground(activeBorderColor)
		} else { // flameGraphPane has focus
			sourceStyle = sourceStyle.BorderForeground(activeBorderColor)
		}
	}

	if m.mode == sourceView {
		if m.paneFocus == sourceCodePane {
			rightPane = sourceStyle.BorderForeground(activeBorderColor).Render(m.source.View())
		} else {
			listStyle = listStyle.BorderForeground(activeBorderColor)
			rightPane = sourceStyle.Render(m.source.View())
		}
	} else if m.mode == graphView {
		rightPane = lipgloss.JoinVertical(lipgloss.Left, m.callersList.View(), m.calleesList.View())
	} else {
		var listSelectedNode *FlameNode
		if selected, ok := m.mainList.SelectedItem().(listItem); ok {
			listSelectedNode = findNodeByName(m.flameGraphRoot, selected.node.Name)
		}

		// The "active" selection passed to the renderer depends on which pane has focus.
		var activeSelection *FlameNode
		if m.paneFocus == flameGraphPane {
			// If navigating the graph, the keyboard selection is king.
			activeSelection = m.flameGraphSelected
		} else {
			// Otherwise, the selection is driven by the list.
			activeSelection = listSelectedNode
		}

		rightPaneWidth := m.source.Width
		totalValue := int64(0)
		if m.flameGraphRoot != nil {
			totalValue = m.flameGraphRoot.Value
		}

		// Render the graph and get layout info for hit detection
		var renderedGraph string
		var newLayout []FlameNodeRenderInfo
		// NOTE: The signature for RenderFlameGraph must be updated to accept `activeSelection`.
		renderedGraph, newLayout = RenderFlameGraph(m.flameGraphRoot, m.flameGraphFocus, activeSelection, m.flameGraphHover, rightPaneWidth, totalValue)
		*m.flameGraphLayout = newLayout // Update layout info in the model

		// Prepare hover details string
		var hoverDetails string
		if m.flameGraphHover != nil {
			currentView := m.profileData.Views[m.currentViewIndex]
			percentOfTotal := 0.0
			if totalValue > 0 {
				percentOfTotal = (float64(m.flameGraphHover.Value) / float64(totalValue)) * 100
			}
			hoverDetails = fmt.Sprintf("Hover: %s | %s | %.1f%% of total",
				m.flameGraphHover.Name,
				formatValue(m.flameGraphHover.Value, currentView.Unit),
				percentOfTotal,
			)
		}

		// Combine graph with an optional details bar at the bottom
		var finalRender strings.Builder
		finalRender.WriteString(renderedGraph)
		detailsBar := m.styles.Status.Width(rightPaneWidth).Render(hoverDetails)
		// We subtract 1 because the status bar takes one line.
		paneContentHeight := m.source.Height
		graphHeight := lipgloss.Height(renderedGraph)

		if hoverDetails != "" {
			// If there's space, add it below. Otherwise, overwrite the last line of the graph.
			if graphHeight < paneContentHeight {
				finalRender.WriteString(strings.Repeat("\n", paneContentHeight-graphHeight-1))
				finalRender.WriteString(detailsBar)
			} else {
				// Overwrite last line
				lines := strings.Split(renderedGraph, "\n")
				if len(lines) > 1 {
					lines[len(lines)-2] = detailsBar
					finalRender.Reset()
					finalRender.WriteString(strings.Join(lines[:len(lines)-1], "\n"))
				}
			}
		}

		rightPane = sourceStyle.Render(finalRender.String())
	}

	panes := lipgloss.JoinHorizontal(lipgloss.Top, listStyle.Render(m.mainList.View()), rightPane)

	var statusText string
	if m.mode == flameGraphView {
		navHelp := "tab focus | ←↑↓→ nav | enter zoom"
		if m.flameGraphFocus != m.flameGraphRoot {
			statusText = m.styles.Status.Render(
				fmt.Sprintf("F1/? help | esc zoom out | %s | f exit flame | q quit", navHelp),
			)
		} else {
			statusText = m.styles.Status.Render(
				fmt.Sprintf("F1/? help | %s | f exit flame | t view | q quit", navHelp),
			)
		}
	} else {
		sortStr := m.currentSortString()
		helpItems := []string{
			"F1/? help",
			fmt.Sprintf("s sort (%s)", sortStr),
			"t view",
			"c mode",
			"p project",
		}

		if !m.isDiffMode {
			helpItems = append(helpItems, "f flame")
		}

		if m.mode == sourceView {
			// Add hint for focus changing after help hint.
			helpItems = slices.Insert(helpItems, 1, "tab focus", "←↑↓→ nav")
		}

		helpItems = append(helpItems, "r resize", "q quit")
		statusText = m.styles.Status.Render(strings.Join(helpItems, " | "))
	}

	var liveHelp string
	if m.isLiveMode {
		if m.isPaused {
			liveHelp = "space resume"
		} else {
			liveHelp = "space pause"
		}
		statusText = m.styles.Status.Render(strings.TrimRight(statusText, " ") + " | " + liveHelp)
	}

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

func min(a, b int64) int64 {
	if a < b {
		return a
	}
	return b
}

func (m model) renderDiagnosticHeader() string {
	var topContent string

	// Show live status first if applicable
	if m.isLiveMode {
		var liveStatus string
		if m.lastError != nil {
			liveStatus = m.styles.DiffNegative.Render(fmt.Sprintf("LIVE (ERROR): %v", m.lastError))
		} else if m.isPaused {
			liveStatus = m.styles.ProjectCode.Render("LIVE (PAUSED)")
		} else {
			liveStatus = m.styles.DiffPositive.Render("LIVE (RUNNING)")
		}
		topContent = lipgloss.JoinHorizontal(lipgloss.Left,
			m.styles.Status.Render(m.sourceInfo),
			" ",
			liveStatus,
		)
	} else {
		topContent = m.styles.Status.Render(m.sourceInfo)
	}

	if m.profileData == nil || len(m.profileData.Views) == 0 {
		return m.styles.Header.Render(topContent)
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

	if diagnosticText == "" {
		return m.styles.Header.Render(topContent)
	}

	return m.styles.Header.Render(lipgloss.JoinVertical(lipgloss.Left, topContent, "  "+diagnosticText))
}
