package main

import (
	tea "github.com/charmbracelet/bubbletea"
)

type UI struct {
	node *Node
}

func NewUI(node *Node) *UI {
	return &UI{node: node}
}

func (ui *UI) Init() tea.Cmd {
	return nil
}

func (ui *UI) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	return ui, nil
}

func (ui *UI) View() string {
	return "TUI mode - not yet implemented\n"
}
