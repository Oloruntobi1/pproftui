// styles.go
package main

import "github.com/charmbracelet/lipgloss"

type Styles struct {
	Base,
	List,
	Source,
	Status,
	Header lipgloss.Style
	DiffPositive,
	DiffNegative,
	ProjectCode lipgloss.Style
}

func defaultStyles() Styles {
	s := Styles{}
	s.Base = lipgloss.NewStyle().Padding(0, 1)

	s.Header = lipgloss.NewStyle().
		Padding(0, 1).
		MarginBottom(1).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("240"))

	s.List = lipgloss.NewStyle().Border(lipgloss.RoundedBorder(), true).BorderForeground(lipgloss.Color("63"))
	s.Source = lipgloss.NewStyle().Border(lipgloss.RoundedBorder(), true).BorderForeground(lipgloss.Color("205"))
	s.Status = lipgloss.NewStyle().
		Background(lipgloss.Color("236")).
		Foreground(lipgloss.Color("250")).
		Padding(0, 1)
	s.DiffPositive = lipgloss.NewStyle().Foreground(lipgloss.Color("10")) // Green
	s.DiffNegative = lipgloss.NewStyle().Foreground(lipgloss.Color("9"))  // Red

	s.ProjectCode = lipgloss.NewStyle().Foreground(lipgloss.Color("86")) // A nice cyan/light blue
	return s
}
