package main

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
)

type configField int

const (
	fieldAgent configField = iota
	fieldDefaultBranch
	fieldMainScreenBranchCount
	fieldDefaultFetch
	fieldIDECommand
	fieldCount
)

type configModel struct {
	inputs      []textinput.Model
	fetchToggle bool
	focused     configField
	err         string
	done        bool
}

func newConfigModel() configModel {
	inputs := make([]textinput.Model, fieldCount)

	var cfg Config
	if loaded, err := LoadConfig(); err == nil {
		cfg = loaded
	}

	agentInput := textinput.New()
	agentInput.Placeholder = defaultAgentCommand
	agentValue := defaultAgentCommand
	if strings.TrimSpace(cfg.AgentCommand) != "" {
		agentValue = cfg.AgentCommand
	}
	agentInput.SetValue(agentValue)
	agentInput.CharLimit = 200
	agentInput.Width = 40
	agentInput.Focus()
	inputs[fieldAgent] = agentInput

	branchInput := textinput.New()
	branchInput.Placeholder = "origin/main"
	if strings.TrimSpace(cfg.NewBranchBaseRef) != "" {
		branchInput.SetValue(cfg.NewBranchBaseRef)
	}
	branchInput.CharLimit = 200
	branchInput.Width = 40
	inputs[fieldDefaultBranch] = branchInput

	branchLimitInput := textinput.New()
	branchLimitInput.Placeholder = strconv.Itoa(defaultMainScreenBranchLimit)
	branchLimitInput.SetValue(strconv.Itoa(cfg.MainScreenBranchLimit))
	branchLimitInput.CharLimit = 4
	branchLimitInput.Width = 10
	inputs[fieldMainScreenBranchCount] = branchLimitInput

	fetchInput := textinput.New()
	inputs[fieldDefaultFetch] = fetchInput

	ideInput := textinput.New()
	ideInput.Placeholder = defaultIDECommand
	ideValue := defaultIDECommand
	if strings.TrimSpace(cfg.IDECommand) != "" {
		ideValue = cfg.IDECommand
	}
	ideInput.SetValue(ideValue)
	ideInput.CharLimit = 200
	ideInput.Width = 40
	inputs[fieldIDECommand] = ideInput

	fetchToggle := true
	if cfg.NewBranchFetchFirst != nil {
		fetchToggle = *cfg.NewBranchFetchFirst
	}

	return configModel{
		inputs:      inputs,
		fetchToggle: fetchToggle,
		focused:     fieldAgent,
	}
}

func (m configModel) Init() tea.Cmd {
	return tea.Batch(tea.ExitAltScreen, tea.ClearScreen)
}

func (m configModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "esc":
			return m, tea.Quit
		case "tab", "down":
			m.inputs[m.focused].Blur()
			m.focused++
			if m.focused >= fieldCount {
				m.focused = 0
			}
			if m.focused != fieldDefaultFetch {
				m.inputs[m.focused].Focus()
			}
			return m, nil
		case "shift+tab", "up":
			m.inputs[m.focused].Blur()
			if m.focused == 0 {
				m.focused = fieldCount - 1
			} else {
				m.focused--
			}
			if m.focused != fieldDefaultFetch {
				m.inputs[m.focused].Focus()
			}
			return m, nil
		case " ":
			if m.focused == fieldDefaultFetch {
				m.fetchToggle = !m.fetchToggle
				return m, nil
			}
		case "enter":
			if err := m.save(); err != nil {
				m.err = err.Error()
				return m, nil
			}
			m.done = true
			return m, tea.Quit
		}
	}

	if m.focused != fieldDefaultFetch {
		var cmd tea.Cmd
		m.inputs[m.focused], cmd = m.inputs[m.focused].Update(msg)
		return m, cmd
	}
	return m, nil
}

func (m configModel) save() error {
	agent := strings.TrimSpace(m.inputs[fieldAgent].Value())
	if agent == "" {
		agent = defaultAgentCommand
	}

	branch := strings.TrimSpace(m.inputs[fieldDefaultBranch].Value())
	branchLimit, err := normalizeMainScreenBranchLimit(m.inputs[fieldMainScreenBranchCount].Value())
	if err != nil {
		return err
	}

	ide := strings.TrimSpace(m.inputs[fieldIDECommand].Value())
	if ide == "" {
		ide = defaultIDECommand
	}

	cfg := Config{
		AgentCommand:          agent,
		NewBranchBaseRef:      branch,
		NewBranchFetchFirst:   &m.fetchToggle,
		IDECommand:            ide,
		MainScreenBranchLimit: branchLimit,
	}
	return SaveConfig(cfg)
}

func (m configModel) View() string {
	if m.done {
		return ""
	}
	var b strings.Builder
	b.WriteString(bannerStyle.Render("WTX Config"))
	b.WriteString("\n\n")

	b.WriteString(m.renderField(fieldAgent, "Agent command:", m.inputs[fieldAgent].View()))
	b.WriteString(m.renderField(fieldDefaultBranch, "Default branch:", m.inputs[fieldDefaultBranch].View()))
	b.WriteString(m.renderField(fieldMainScreenBranchCount, "Main screen branch count:", m.inputs[fieldMainScreenBranchCount].View()))
	b.WriteString(m.renderFetchField())
	b.WriteString(m.renderField(fieldIDECommand, "IDE command:", m.inputs[fieldIDECommand].View()))

	b.WriteString("\n")
	b.WriteString("Use tab/shift+tab to navigate, space to toggle, enter to save, esc to cancel.\n")

	if m.err != "" {
		b.WriteString("\n")
		b.WriteString(errorStyle.Render(fmt.Sprintf("Error: %s", m.err)))
		b.WriteString("\n")
	}
	return b.String()
}

func (m configModel) renderField(field configField, label string, inputView string) string {
	cursor := "  "
	if m.focused == field {
		cursor = "> "
	}
	return fmt.Sprintf("%s%s\n  %s\n", cursor, label, inputStyle.Render(inputView))
}

func (m configModel) renderFetchField() string {
	cursor := "  "
	if m.focused == fieldDefaultFetch {
		cursor = "> "
	}
	checked := "x"
	if !m.fetchToggle {
		checked = " "
	}
	return fmt.Sprintf("%sFetch before create: [%s]\n", cursor, checked)
}
