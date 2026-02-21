package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type tmuxAction string

const (
	tmuxActionShellSplit tmuxAction = "shell_split"
	tmuxActionShellTab   tmuxAction = "shell_tab"
	tmuxActionIDE        tmuxAction = "ide"
	tmuxActionPR         tmuxAction = "pr"
)

type tmuxActionItem struct {
	Label    string
	Action   tmuxAction
	Disabled bool
}

type tmuxActionsModel struct {
	basePath string
	items    []tmuxActionItem
	index    int
	chosen   tmuxAction
	cancel   bool
	prKnown  bool
}

func newTmuxActionsModel(basePath string, prAvailable bool, canOpenITermTab bool) tmuxActionsModel {
	items := []tmuxActionItem{
		{Label: "Open shell (split down)", Action: tmuxActionShellSplit},
	}
	if canOpenITermTab {
		items = append(items, tmuxActionItem{Label: "Open shell (new iTerm tab)", Action: tmuxActionShellTab})
	}
	items = append(items,
		tmuxActionItem{Label: "Open IDE", Action: tmuxActionIDE},
		tmuxActionItem{Label: "Open PR", Action: tmuxActionPR, Disabled: !prAvailable},
	)
	return tmuxActionsModel{
		basePath: basePath,
		items:    items,
	}
}

type prAvailabilityMsg struct {
	available bool
}

func (m tmuxActionsModel) Init() tea.Cmd {
	return checkPRAvailabilityCmd(m.basePath)
}

func (m tmuxActionsModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case prAvailabilityMsg:
		m.prKnown = true
		for i := range m.items {
			if m.items[i].Action == tmuxActionPR {
				m.items[i].Disabled = !msg.available
				break
			}
		}
		return m, nil
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "esc":
			m.cancel = true
			return m, tea.Quit
		case "up", "k":
			if m.index > 0 {
				m.index--
			}
			return m, nil
		case "down", "j":
			if m.index < len(m.items)-1 {
				m.index++
			}
			return m, nil
		case "enter":
			if len(m.items) == 0 {
				m.cancel = true
				return m, tea.Quit
			}
			selected := m.items[m.index]
			if selected.Disabled {
				return m, nil
			}
			m.chosen = selected.Action
			return m, tea.Quit
		}
	}
	return m, nil
}

func (m tmuxActionsModel) View() string {
	var b strings.Builder
	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#7D56F4"))
	selectedStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#7D56F4"))
	normalStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("251"))
	disabledStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	dimStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("245"))

	b.WriteString(titleStyle.Render("Actions"))
	b.WriteString("\n")
	b.WriteString(dimStyle.Render(strings.TrimSpace(m.basePath)))
	b.WriteString("\n")
	b.WriteString(dimStyle.Render("────────────────────────────────────"))
	b.WriteString("\n")
	for i, item := range m.items {
		prefix := "  "
		if i == m.index {
			prefix = "> "
		}
		label := item.Label
		if item.Disabled {
			if item.Action == tmuxActionPR && !m.prKnown {
				label += " (checking...)"
			} else {
				label += " (unavailable)"
			}
		}
		switch {
		case item.Disabled:
			b.WriteString(disabledStyle.Render(prefix + label))
		case i == m.index:
			b.WriteString(selectedStyle.Render(prefix + label))
		default:
			b.WriteString(normalStyle.Render(prefix + label))
		}
		b.WriteString("\n")
	}
	b.WriteString("\n")
	b.WriteString(dimStyle.Render("↑/↓ navigate • enter select • esc cancel"))
	return b.String()
}

func runTmuxActions(args []string) error {
	basePath := ""
	if len(args) > 0 {
		basePath = strings.TrimSpace(args[0])
	}
	if basePath == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return err
		}
		basePath = cwd
	}

	canOpenITermTab := canOpenShellInITermTab()
	program := tea.NewProgram(newTmuxActionsModel(basePath, false, canOpenITermTab))
	finalModel, err := program.Run()
	if err != nil {
		return err
	}
	m := finalModel.(tmuxActionsModel)
	if m.cancel || m.chosen == "" {
		return nil
	}
	switch m.chosen {
	case tmuxActionShellSplit:
		cmd := exec.Command("tmux", "split-window", "-v", "-p", "50", "-c", basePath)
		return cmd.Run()
	case tmuxActionShellTab:
		return openShellInITermTab(basePath)
	case tmuxActionIDE:
		clearPopupScreen()
		return runIDEPicker([]string{basePath})
	case tmuxActionPR:
		cmd := exec.Command("gh", "pr", "view", "--web")
		cmd.Dir = basePath
		return cmd.Run()
	default:
		return nil
	}
}

func hasCurrentPR(path string) bool {
	if strings.TrimSpace(path) == "" {
		return false
	}
	if _, err := exec.LookPath("gh"); err != nil {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 1200*time.Millisecond)
	defer cancel()
	cmd := exec.CommandContext(ctx, "gh", "pr", "view", "--json", "number")
	cmd.Dir = path
	out, err := cmd.Output()
	if err != nil {
		return false
	}
	var payload struct {
		Number int `json:"number"`
	}
	if err := json.Unmarshal(out, &payload); err != nil {
		return false
	}
	return payload.Number > 0
}

func checkPRAvailabilityCmd(path string) tea.Cmd {
	return func() tea.Msg {
		return prAvailabilityMsg{available: hasCurrentPR(path)}
	}
}

func canOpenShellInITermTab() bool {
	if iTermIntegrationDisabled() {
		return false
	}
	if strings.TrimSpace(os.Getenv("TERM_PROGRAM")) != "iTerm.app" {
		return false
	}
	if _, err := exec.LookPath("osascript"); err != nil {
		return false
	}
	return true
}

func openShellInITermTab(path string) error {
	path = strings.TrimSpace(path)
	if path == "" {
		return fmt.Errorf("missing path")
	}
	script := `
on run argv
	set p to item 1 of argv
	tell application "iTerm"
		activate
		if (count of windows) is 0 then
			create window with default profile
		end if
		tell current window
			create tab with default profile
			tell current session
				write text "cd " & quoted form of p
			end tell
		end tell
	end tell
end run
`
	cmd := exec.Command("osascript", "-e", script, "--", path)
	return cmd.Run()
}

func clearPopupScreen() {
	fmt.Print("\x1b[2J\x1b[H")
}

func tmuxActionsPopupCommand(wtxBin string) string {
	return fmt.Sprintf("%s tmux-actions #{q:pane_current_path}", shellQuote(wtxBin))
}
