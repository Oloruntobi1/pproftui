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

	if strings.HasPrefix(arg, "http://") || strings.HasPrefix(arg, "https://") {
		// It's a URL, fetch it
		fmt.Println("Fetching profile from URL:", arg)
		resp, err := http.Get(arg)
		if err != nil {
			log.Fatalf("Failed to fetch profile from URL: %v", err)
		}
		// The response body is an io.Reader! We can stream it.
		reader = resp.Body
		defer resp.Body.Close()
	} else {
		// It's a file path
		file, err := os.Open(arg)
		if err != nil {
			log.Fatalf("Failed to open profile file: %v", err)
		}
		reader = file
		defer file.Close()
	}

	functions, err := ParseProfile(reader) // <-- Pass the reader
	if err != nil {
		log.Fatal(err)
	}
	if len(functions) == 0 {
		log.Fatal("No function data found in profile.")
	}

	m := newModel(functions)
	p := tea.NewProgram(m, tea.WithAltScreen(), tea.WithMouseCellMotion())

	if err := p.Start(); err != nil {
		log.Fatal("Error running program:", err)
	}
}
