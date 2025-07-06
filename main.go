// main.go
package main

import (
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

func main() {
	modulePath := flag.String("module-path", "", "Root module path of your project (e.g., github.com/user/repo) to highlight relevant code.")

	liveURL := flag.String("live", "", "HTTP URL of a live pprof endpoint to poll (e.g., http://localhost:6060/debug/pprof/profile?seconds=5).")
	refreshInterval := flag.Duration("refresh", 5*time.Second, "Refresh interval for live mode.")

	flag.Parse()

	if *liveURL != "" {
		// In live mode, we initialize the model without data.
		// The first fetch will happen as a command.
		// The sourceInfo will just be the URL.
		m := newModel(nil, *liveURL)
		m.isLiveMode = true
		m.liveURL = *liveURL
		m.refreshInterval = *refreshInterval

		if *modulePath != "" {
			m.modulePath = *modulePath
		}

		p := tea.NewProgram(m, tea.WithAltScreen(), tea.WithMouseAllMotion())
		if _, err := p.Run(); err != nil {
			log.Fatal("Error running program:", err)
		}
		return
	}

	args := flag.Args()

	if len(args) < 1 {
		fmt.Println("Usage: pproftui [--module-path <your_module>] <profile_file_or_url>")
		fmt.Println("       pproftui [--module-path <your_module>] <before_profile> <after_profile>")
		flag.PrintDefaults()
		os.Exit(1)
	}

	var sourceInfo string
	var profileData *ProfileData
	var err error

	if len(args) == 1 {
		// Single profile mode
		sourceInfo = fmt.Sprintf("Source: %s", args[0])
		reader, closer := getReaderForArg(args[0])
		defer closer.Close()
		profileData, err = ParsePprofFile(reader)
	} else if len(args) == 2 {
		// Diff mode
		sourceInfo = fmt.Sprintf("Diff: %s vs %s", args[0], args[1])
		readerBefore, closerBefore := getReaderForArg(args[0])
		defer closerBefore.Close()
		readerAfter, closerAfter := getReaderForArg(args[1])
		defer closerAfter.Close()
		profileData, err = DiffPprofFiles(readerBefore, readerAfter)
	} else {
		log.Fatal("Invalid number of arguments.")
	}

	if err != nil {
		log.Fatal(err)
	}

	if *modulePath != "" {
		annotateProjectCode(profileData, *modulePath)
	}

	m := newModel(profileData, sourceInfo)
	p := tea.NewProgram(m, tea.WithAltScreen(), tea.WithMouseAllMotion())
	if _, err := p.Run(); err != nil {
		log.Fatal("Error running program:", err)
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
