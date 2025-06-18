// main.go
package main

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Println("Usage:")
		fmt.Println("  tui-profiler <profile_file_or_url>")
		fmt.Println("  tui-profiler <before_profile> <after_profile>")
		os.Exit(1)
	}

	if len(os.Args) == 2 {
		// Single profile mode
		reader, closer := getReaderForArg(os.Args[1])
		defer closer.Close()
		profileData, err := ParsePprofFile(reader)
		if err != nil {
			log.Fatal(err)
		}
		m := newModel(profileData)
		p := tea.NewProgram(m, tea.WithAltScreen(), tea.WithMouseCellMotion())
		if err := p.Start(); err != nil {
			log.Fatal("Error running program:", err)
		}
	} else if len(os.Args) == 3 {
		// Diff mode
		fmt.Println("Starting in diff mode...")
		readerBefore, closerBefore := getReaderForArg(os.Args[1])
		defer closerBefore.Close()
		readerAfter, closerAfter := getReaderForArg(os.Args[2])
		defer closerAfter.Close()

		diffData, err := DiffPprofFiles(readerBefore, readerAfter)
		if err != nil {
			log.Fatal(err)
		}
		// We can reuse our existing model, as it just needs a ProfileData object
		m := newModel(diffData)
		p := tea.NewProgram(m, tea.WithAltScreen(), tea.WithMouseCellMotion())
		if err := p.Start(); err != nil {
			log.Fatal("Error running program:", err)
		}
	} else {
		log.Fatal("Invalid number of arguments.")
	}
}

// getReaderForArg is a helper to avoid code duplication.
func getReaderForArg(arg string) (io.Reader, io.Closer) {
	if strings.HasPrefix(arg, "http://") || strings.HasPrefix(arg, "https://") {
		fmt.Println("Fetching profile from:", arg)
		resp, err := http.Get(arg)
		if err != nil {
			log.Fatalf("Failed to fetch profile from URL: %v", err)
		}
		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			log.Fatalf("Endpoint returned status %s: %s", resp.Status, string(body))
		}
		return resp.Body, resp.Body
	}

	file, err := os.Open(arg)
	if err != nil {
		log.Fatalf("Failed to open profile file: %v", err)
	}
	return file, file
}
