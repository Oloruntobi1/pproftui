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
	var closer io.Closer // We need to manage closing the connection or file

	if strings.HasPrefix(arg, "http://") || strings.HasPrefix(arg, "https://") {
		// It's a URL. Fetch the profile data over the network.
		fmt.Println("Connecting to live profile endpoint...")
		fmt.Println("Profiling for the duration specified in the URL (e.g., ?seconds=5)...")

		resp, err := http.Get(arg)
		if err != nil {
			log.Fatalf("Failed to fetch profile from URL: %v", err)
		}

		if resp.StatusCode != http.StatusOK {
			log.Fatalf("Failed to fetch profile: endpoint returned status %s", resp.Status)
		}

		fmt.Println("Profile data received. Analyzing...")
		reader = resp.Body
		closer = resp.Body // The response body needs to be closed
	} else {
		// It's a file path. Open it from the local disk.
		file, err := os.Open(arg)
		if err != nil {
			log.Fatalf("Failed to open profile file: %v", err)
		}
		reader = file
		closer = file // The file needs to be closed
	}

	// Make sure we close the file or the HTTP body when main exits.
	defer closer.Close()

	// Our smart parser just takes the reader and doesn't care where it came from.
	profileData, err := ParsePprofFile(reader)
	if err != nil {
		log.Fatal(err)
	}

	m := newModel(profileData)
	p := tea.NewProgram(m, tea.WithAltScreen(), tea.WithMouseCellMotion())

	if err := p.Start(); err != nil {
		log.Fatal("Error running program:", err)
	}
}
