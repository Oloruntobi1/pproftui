package main

import (
	"fmt"
	"sort"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// FlameNodeRenderInfo holds the position and size of a rendered flame graph node,
// used for hit detection (hovering).
type FlameNodeRenderInfo struct {
	Node  *FlameNode
	X, Y  int
	Width int
}

// remainderItem is used for sorting during the apportionment process.
type remainderItem struct {
	Index  int
	Value  int
	Remain float64
}

// apportion distributes a total width among a set of floating-point values
// using the Largest Remainder Method, ensuring the sum of integer widths
// equals the total width.
func apportion(values []float64, totalWidth int) []int {
	if totalWidth <= 0 {
		return make([]int, len(values))
	}

	items := make([]remainderItem, len(values))
	sumFloats := 0.0
	for _, v := range values {
		sumFloats += v
	}

	if sumFloats == 0 {
		return make([]int, len(values))
	}

	// Calculate ideal widths and initial integer parts
	sumInts := 0
	for i, v := range values {
		idealWidth := v / sumFloats * float64(totalWidth)
		items[i].Index = i
		items[i].Value = int(idealWidth)
		items[i].Remain = idealWidth - float64(items[i].Value)
		sumInts += items[i].Value
	}

	// Distribute the remainder
	remainder := totalWidth - sumInts
	if remainder > 0 {
		// Sort by remainder descending to give extras to the largest fractions
		sort.Slice(items, func(i, j int) bool {
			return items[i].Remain > items[j].Remain
		})

		for i := 0; i < remainder && i < len(items); i++ {
			items[i].Value++
		}
	}

	// Restore original order
	sort.Slice(items, func(i, j int) bool {
		return items[i].Index < items[j].Index
	})

	result := make([]int, len(values))
	for i := range items {
		result[i] = items[i].Value
	}

	return result
}

// findNodeByName searches for a node with the given name in the flame graph.
func findNodeByName(root *FlameNode, targetName string) *FlameNode {
	if root == nil {
		return nil
	}
	if root.Name == targetName {
		return root
	}
	for _, child := range root.Children {
		if found := findNodeByName(child, targetName); found != nil {
			return found
		}
	}
	return nil
}

// findPathToNode returns the slice of nodes from the root to the target node.
func findPathToNode(target *FlameNode) []*FlameNode {
	if target == nil {
		return nil
	}
	path := []*FlameNode{}
	for curr := target; curr != nil; curr = curr.Parent {
		path = append(path, curr)
	}
	// Reverse the path to be from root to target
	for i, j := 0, len(path)-1; i < j; i, j = i+1, j-1 {
		path[i], path[j] = path[j], path[i]
	}
	return path
}

type FlameGraphLayoutInfo struct {
	Offset int
	Width  int
}

// generateFlameGraphLayout calculates the offset and width for every node.
func generateFlameGraphLayout(root, focusNode *FlameNode, totalWidth int) map[*FlameNode]FlameGraphLayoutInfo {
	layout := make(map[*FlameNode]FlameGraphLayoutInfo)
	if root == nil || focusNode == nil || totalWidth <= 0 {
		return layout
	}

	// The focus node and its parents get 100% width
	path := findPathToNode(focusNode)
	for _, node := range path {
		layout[node] = FlameGraphLayoutInfo{Offset: 0, Width: totalWidth}
	}

	// Use a queue to process nodes level by level (BFS)
	queue := []*FlameNode{focusNode}

	for len(queue) > 0 {
		parent := queue[0]
		queue = queue[1:]

		if len(parent.Children) == 0 {
			continue
		}

		parentLayout := layout[parent]
		childValues := make([]float64, len(parent.Children))
		for i, child := range parent.Children {
			childValues[i] = float64(child.Value)
		}

		childWidths := apportion(childValues, parentLayout.Width)

		childOffset := parentLayout.Offset
		for i, child := range parent.Children {
			width := childWidths[i]
			if width > 0 {
				layout[child] = FlameGraphLayoutInfo{Offset: childOffset, Width: width}
				queue = append(queue, child)
			}
			childOffset += width
		}
	}

	return layout
}

// getColorForPercentage returns a color based on how "hot" a function is.
func getColorForPercentage(percentage float64) lipgloss.Color {
	switch {
	case percentage >= 10.0: // Very hot - red
		return lipgloss.Color("196")
	case percentage >= 5.0: // Hot - orange
		return lipgloss.Color("202")
	case percentage >= 2.0: // Warm - yellow-orange
		return lipgloss.Color("208")
	case percentage >= 1.0: // Medium - yellow
		return lipgloss.Color("220")
	case percentage >= 0.5: // Cool - light green
		return lipgloss.Color("154")
	default: // Very cool - green
		return lipgloss.Color("82")
	}
}

// RenderFlameGraph renders the entire flame graph as a string.
func RenderFlameGraph(root, focusNode, viewNode, hoveredNode *FlameNode, termWidth int, totalValue int64) (string, []FlameNodeRenderInfo) {
	if root == nil || focusNode == nil || focusNode.Value == 0 || termWidth <= 0 {
		return "No data to render in flame graph.", nil
	}

	layout := generateFlameGraphLayout(root, focusNode, termWidth)
	depthLevels := groupNodesByRelativeDepth(focusNode)
	renderInfos := make([]FlameNodeRenderInfo, 0)

	// To keep rendering in depth order
	maxDepth := 0
	for depth := range depthLevels {
		if depth > maxDepth {
			maxDepth = depth
		}
	}

	focusPathSet := make(map[*FlameNode]struct{})
	// The focus path is now just the focus node itself, since it's the root of our view.
	focusPathSet[focusNode] = struct{}{}
	// We find the path to the focus node to correctly dim its parents if they are visible
	for _, node := range findPathToNode(focusNode) {
		focusPathSet[node] = struct{}{}
	}

	var b strings.Builder

	for depth := 0; depth <= maxDepth; depth++ {
		nodes, exists := depthLevels[depth]
		if !exists {
			continue
		}

		// Sort nodes in this row by layout offset
		sort.Slice(nodes, func(i, j int) bool {
			li, ok_i := layout[nodes[i]]
			lj, ok_j := layout[nodes[j]]
			if !ok_i || !ok_j {
				return false
			}
			return li.Offset < lj.Offset
		})

		cursor := 0
		for _, node := range nodes {
			nodeLayout, ok := layout[node]
			if !ok || nodeLayout.Width <= 0 {
				continue
			}

			renderInfos = append(renderInfos, FlameNodeRenderInfo{
				Node:  node,
				X:     nodeLayout.Offset,
				Y:     depth, // Use the relative depth for the Y coordinate
				Width: nodeLayout.Width,
			})

			padding := nodeLayout.Offset - cursor
			if padding > 0 {
				b.WriteString(strings.Repeat(" ", padding))
			}

			// ... The rest of the function remains the same ...
			percent := 0.0
			if totalValue > 0 {
				percent = (float64(node.Value) / float64(totalValue)) * 100
			}
			color := getColorForPercentage(percent)
			style := lipgloss.NewStyle().
				Background(color).
				Foreground(lipgloss.Color("232"))

			if _, inFocusPath := focusPathSet[node]; !inFocusPath {
				style = style.Faint(true)
			}
			if node == hoveredNode {
				// Hover style overrides other styles
				style = lipgloss.NewStyle().Background(lipgloss.Color("228")).Foreground(lipgloss.Color("0")) // Light yellow
			} else if viewNode != nil && node.Name == viewNode.Name {
				style = style.Underline(true).Bold(true).Background(lipgloss.Color("99"))
			}

			// Truncate name logic
			parts := strings.Split(node.Name, "/")
			name := parts[len(parts)-1]
			label := fmt.Sprintf("%s (%.1f%%)", name, percent)
			if lipgloss.Width(label) > nodeLayout.Width {
				label = name
			}
			if lipgloss.Width(label) > nodeLayout.Width {
				label = label[:nodeLayout.Width]
			}

			bar := style.Render(label)
			barWidth := lipgloss.Width(bar)
			if barWidth < nodeLayout.Width {
				bar += style.Render(strings.Repeat(" ", nodeLayout.Width-barWidth))
			} else if barWidth > nodeLayout.Width {
				bar = lipgloss.NewStyle().SetString(bar).MaxWidth(nodeLayout.Width).String()
			}

			b.WriteString(bar)
			cursor = nodeLayout.Offset + nodeLayout.Width
		}
		b.WriteString("\n")
	}

	return b.String(), renderInfos
}

func groupNodesByRelativeDepth(startNode *FlameNode) map[int][]*FlameNode {
	levels := make(map[int][]*FlameNode)
	if startNode == nil {
		return levels
	}

	var visit func(n *FlameNode, depth int)
	visit = func(n *FlameNode, depth int) {
		if n == nil {
			return
		}
		levels[depth] = append(levels[depth], n)
		for _, child := range n.Children {
			visit(child, depth+1)
		}
	}
	// Start the traversal from the provided startNode at relative depth 0.
	visit(startNode, 0)
	return levels
}
