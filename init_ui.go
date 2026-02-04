package main

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
)

type initModel struct {
	input textinput.Model
	err   string
	done  bool
}

func newInitModel() initModel {
	ti := textinput.New()
	current := defaultAgentCommand
	if cfg, err := LoadConfig(); err == nil && strings.TrimSpace(cfg.AgentCommand) != "" {
		current = cfg.AgentCommand
	}
	ti.Placeholder = defaultAgentCommand
	ti.SetValue(current)
	ti.CharLimit = 200
	ti.Width = 40
	ti.Focus()
	return initModel{input: ti}
}

func (m initModel) Init() tea.Cmd {
	return tea.Batch(tea.ExitAltScreen, tea.ClearScreen)
}

func (m initModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "esc":
			return m, tea.Quit
		case "enter":
			value := strings.TrimSpace(m.input.Value())
			if value == "" {
				value = defaultAgentCommand
			}
			if err := SaveConfig(Config{AgentCommand: value}); err != nil {
				m.err = err.Error()
				return m, nil
			}
			m.done = true
			return m, tea.Quit
		}
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

func (m initModel) View() string {
	if m.done {
		return ""
	}
	var b strings.Builder
	b.WriteString(bannerStyle.Render("WTX"))
	b.WriteString("\n\n")
	b.WriteString("AI agent command when selecting a worktree:\n")
	b.WriteString(inputStyle.Render(m.input.View()))
	b.WriteString("\n")
	b.WriteString("Press enter to save, esc to cancel.\n")
	if m.err != "" {
		b.WriteString("\n")
		b.WriteString(errorStyle.Render(fmt.Sprintf("Error: %s", m.err)))
		b.WriteString("\n")
	}
	return b.String()
}
