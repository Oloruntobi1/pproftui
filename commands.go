// commands.go
package main

import (
	"fmt"
	"io"
	"net/http"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// tickerCmd sends a tickMsg at a given interval
func tickerCmd(interval time.Duration) tea.Cmd {
	return tea.Tick(interval, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

// fetchProfileCmd performs the HTTP GET, parsing, and annotation in the background.
func fetchProfileCmd(url, modulePath string) tea.Cmd {
	return func() tea.Msg {
		// Fetch the profile data from the URL
		resp, err := http.Get(url)
		if err != nil {
			return profileUpdateErr{fmt.Errorf("http get: %w", err)}
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			return profileUpdateErr{fmt.Errorf("bad status: %s: %s", resp.Status, string(body))}
		}

		// Parse the new data
		profileData, err := ParsePprofFile(resp.Body)
		if err != nil {
			return profileUpdateErr{fmt.Errorf("parse failed: %w", err)}
		}

		// Annotate with project code if path is provided.
		// This ensures live updates respect the module path.
		if modulePath != "" {
			annotateProjectCode(profileData, modulePath)
		}

		return profileUpdateMsg{data: profileData}
	}
}
