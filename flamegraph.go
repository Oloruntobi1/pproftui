// flamegraph.go
package main

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// A simple color palette for the flame graph
var flameColors = []lipgloss.Color{
	lipgloss.Color("196"), lipgloss.Color("202"), lipgloss.Color("208"),
	lipgloss.Color("214"), lipgloss.Color("220"), lipgloss.Color("226"),
	lipgloss.Color("154"), lipgloss.Color("118"), lipgloss.Color("82"),
}

// getColorForPercentage returns a color based on how "hot" a function is
// Hot (high percentage) = red/orange, Cool (low percentage) = yellow/green
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

// RenderFlameGraph takes the root node and total width and returns a rendered string.
func RenderFlameGraph(root *FlameNode, width int) string {
	if root.Value == 0 {
		return "No data to render in flame graph."
	}

	var b strings.Builder
	// The depth parameter helps us pick a color
	renderFlameNode(&b, root, 0, width, root.Value, 0)
	return b.String()
}

func renderFlameNode(b *strings.Builder, node *FlameNode, depth, termWidth int, totalValue int64, offset int) {
	// Calculate the width of this node's bar as a percentage of the total
	nodeWidth := int(float64(node.Value) / float64(totalValue) * float64(termWidth))
	if nodeWidth <= 0 {
		return // Too small to render
	}

	// Calculate percentage of total
	percentage := float64(node.Value) / float64(totalValue) * 100
	
	// Pick a color based on "hotness" (percentage of total)
	color := getColorForPercentage(percentage)
	style := lipgloss.NewStyle().
		Background(color).
		Foreground(lipgloss.Color("232")) // Dark text for contrast
	
	// Create label with function name and percentage
	labelText := fmt.Sprintf("%s (%.1f%%)", node.Name, percentage)
	
	// Truncate the label to fit the bar width
	if len(labelText) > nodeWidth {
		// Try just the function name if the full label is too long
		if len(node.Name) <= nodeWidth {
			labelText = node.Name
		} else {
			labelText = labelText[:nodeWidth]
		}
	}

	// Create the bar string
	bar := style.Render(labelText)
	padding := strings.Repeat(" ", nodeWidth-len(labelText))
	bar += style.Render(padding)

	// Add the offset (padding from the left) and the bar to the output
	b.WriteString(strings.Repeat(" ", offset))
	b.WriteString(bar)
	b.WriteString("\n")

	// Recursively render children
	childOffset := offset
	for _, child := range node.Children {
		renderFlameNode(b, child, depth+1, termWidth, totalValue, childOffset)
		// The next child starts where the previous one left off, in terms of horizontal space
		childOffset += int(float64(child.Value) / float64(totalValue) * float64(termWidth))
	}
}
