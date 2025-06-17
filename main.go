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
		fmt.Println("Usage: tui-profiler <path/to/profile.pprof | http://host/path/to/profile>")
		os.Exit(1)
	}

	arg := os.Args[1]
	var reader io.Reader
	var closer io.Closer

	if strings.HasPrefix(arg, "http://") || strings.HasPrefix(arg, "https://") {
		fmt.Println("Fetching profile from URL:", arg)
		resp, err := http.Get(arg)
		if err != nil {
			log.Fatalf("Failed to fetch profile from URL: %v", err)
		}
		reader = resp.Body
		closer = resp.Body
	} else {
		file, err := os.Open(arg)
		if err != nil {
			log.Fatalf("Failed to open profile file: %v", err)
		}
		reader = file
		closer = file
	}
	defer closer.Close()

	// Use the new parser
	profileData, err := ParsePprofFile(reader)
	if err != nil {
		log.Fatal(err)
	}

	m := newModel(profileData) // Pass the full data object to the model
	p := tea.NewProgram(m, tea.WithAltScreen(), tea.WithMouseCellMotion())

	if err := p.Start(); err != nil {
		log.Fatal("Error running program:", err)
	}
}
