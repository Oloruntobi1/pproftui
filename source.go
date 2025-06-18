package main

import (
	"bytes"
	"fmt"
	"os"
	"strings"

	"github.com/alecthomas/chroma/v2/quick"
)

// getHighlightedSource reads a file, highlights it, and adds line numbers and an arrow.
func getHighlightedSource(filePath string, targetLine int) string {
	if filePath == "" {
		return "No source file available."
	}

	content, err := os.ReadFile(filePath)
	if err != nil {
		return fmt.Sprintf("Error reading file %s:\n%v", filePath, err)
	}

	// Use Chroma for syntax highlighting
	var highlighted bytes.Buffer
	err = quick.Highlight(&highlighted, string(content), "go", "terminal256", "monokai")
	if err != nil {
		// Fallback to plain text if highlighting fails
		highlighted.WriteString(string(content))
	}

	lines := strings.Split(highlighted.String(), "\n")
	var result strings.Builder

	for i, line := range lines {
		lineNumber := i + 1
		lineHeader := fmt.Sprintf("%4d | ", lineNumber)
		if lineNumber == targetLine {
			// Add an arrow to the target line
			lineHeader = "  -> | "
		}
		result.WriteString(lineHeader + line + "\n")
	}

	return result.String()
}
