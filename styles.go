package main

import "github.com/charmbracelet/lipgloss"

type Styles struct {
	Base,
	List,
	Source,
	Status lipgloss.Style
}

func defaultStyles() Styles {
	s := Styles{}
	s.Base = lipgloss.NewStyle().Padding(0, 1)
	s.List = lipgloss.NewStyle().Border(lipgloss.RoundedBorder(), true).BorderForeground(lipgloss.Color("63"))
	s.Source = lipgloss.NewStyle().Border(lipgloss.RoundedBorder(), true).BorderForeground(lipgloss.Color("205"))
	s.Status = lipgloss.NewStyle().
		Background(lipgloss.Color("236")).
		Foreground(lipgloss.Color("250")).
		Padding(0, 1)
	return s
}