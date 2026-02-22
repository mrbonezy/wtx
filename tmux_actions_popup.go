package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type tmuxAction string

const (
	tmuxActionShellSplit tmuxAction = "shell_split"
	tmuxActionShellTab   tmuxAction = "shell_tab"
	tmuxActionIDE        tmuxAction = "ide"
	tmuxActionPR         tmuxAction = "pr"
	tmuxActionBack       tmuxAction = "back_to_wtx"
)

type tmuxActionItem struct {
	Label    string
	Action   tmuxAction
	Keywords string
	Disabled bool
}

type tmuxActionsModel struct {
	basePath   string
	items      []tmuxActionItem
	filtered   []int
	index      int
	query      string
	chosen     tmuxAction
	cancel     bool
	prKnown    bool
	updateHint string
}

func newTmuxActionsModel(basePath string, prAvailable bool, canOpenITermTab bool) tmuxActionsModel {
	items := []tmuxActionItem{
		{Label: "Back to WTX (stop agent)", Action: tmuxActionBack, Keywords: "back return wtx unlock ctrl+w"},
		{Label: "Open shell (split down)", Action: tmuxActionShellSplit, Keywords: "shell split pane ctrl+s s"},
	}
	if canOpenITermTab {
		items = append(items, tmuxActionItem{Label: "Open shell (new iTerm tab)", Action: tmuxActionShellTab, Keywords: "shell tab iterm"})
	}
	items = append(items,
		tmuxActionItem{Label: "Open IDE", Action: tmuxActionIDE, Keywords: "ide editor code ctrl+l l"},
		tmuxActionItem{Label: "Open PR", Action: tmuxActionPR, Keywords: "pr pull request github ctrl+p p", Disabled: !prAvailable},
	)
	model := tmuxActionsModel{
		basePath: basePath,
		items:    items,
	}
	model.rebuildFiltered()
	return model
}

type prAvailabilityMsg struct {
	available bool
}

func (m tmuxActionsModel) Init() tea.Cmd {
	return tea.Batch(checkPRAvailabilityCmd(m.basePath), checkInteractiveUpdateHintCmd())
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
		m.rebuildFiltered()
		return m, nil
	case interactiveUpdateHintMsg:
		m.updateHint = strings.TrimSpace(msg.hint)
		return m, nil
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "esc":
			m.cancel = true
			return m, tea.Quit
		case "backspace":
			if m.query != "" {
				_, size := utf8.DecodeLastRuneInString(m.query)
				if size > 0 {
					m.query = m.query[:len(m.query)-size]
				}
				m.rebuildFiltered()
			}
			return m, nil
		case "ctrl+u":
			if m.query != "" {
				m.query = ""
				m.rebuildFiltered()
			}
			return m, nil
		case "up", "k":
			if len(m.filtered) > 0 && m.index > 0 {
				m.index--
			}
			return m, nil
		case "down", "j":
			if len(m.filtered) > 0 && m.index < len(m.filtered)-1 {
				m.index++
			}
			return m, nil
		case "enter":
			selected, ok := m.selectedItem()
			if !ok {
				m.cancel = true
				return m, tea.Quit
			}
			if selected.Disabled {
				return m, nil
			}
			m.chosen = selected.Action
			return m, tea.Quit
		default:
			if msg.Type == tea.KeyRunes {
				runes := msg.Runes
				if len(runes) == 0 {
					return m, nil
				}
				m.query += strings.ToLower(string(runes))
				m.rebuildFiltered()
				return m, nil
			}
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
	b.WriteString(dimStyle.Render("Search: " + m.query))
	b.WriteString("\n")
	b.WriteString(dimStyle.Render("────────────────────────────────────"))
	b.WriteString("\n")
	if len(m.filtered) == 0 {
		b.WriteString(disabledStyle.Render("  No matching actions"))
		b.WriteString("\n")
	}
	for listIndex, itemIndex := range m.filtered {
		item := m.items[itemIndex]
		prefix := "  "
		if listIndex == m.index {
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
		case listIndex == m.index:
			b.WriteString(selectedStyle.Render(prefix + label))
		default:
			b.WriteString(normalStyle.Render(prefix + label))
		}
		b.WriteString("\n")
	}
	b.WriteString("\n")
	b.WriteString(dimStyle.Render("type to filter • ↑/↓ navigate • enter select • esc cancel"))
	if m.updateHint != "" {
		b.WriteString("\n")
		b.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("240")).Render(m.updateHint))
	}
	return b.String()
}

func (m *tmuxActionsModel) rebuildFiltered() {
	query := strings.TrimSpace(strings.ToLower(m.query))
	indices := make([]int, 0, len(m.items))
	for i := range m.items {
		if query == "" || actionMatchesQuery(m.items[i], query) {
			indices = append(indices, i)
		}
	}
	m.filtered = indices
	if len(m.filtered) == 0 {
		m.index = 0
		return
	}
	if m.index < 0 {
		m.index = 0
	}
	if m.index >= len(m.filtered) {
		m.index = len(m.filtered) - 1
	}
}

func actionMatchesQuery(item tmuxActionItem, query string) bool {
	query = strings.TrimSpace(strings.ToLower(query))
	if query == "" {
		return true
	}
	corpus := strings.ToLower(strings.TrimSpace(item.Label + " " + item.Keywords + " " + string(item.Action)))
	if strings.Contains(corpus, query) {
		return true
	}
	parts := actionTokenSplitRe.Split(corpus, -1)
	for _, part := range parts {
		if strings.HasPrefix(part, query) {
			return true
		}
	}
	return false
}

func (m tmuxActionsModel) selectedItem() (tmuxActionItem, bool) {
	if len(m.filtered) == 0 || m.index < 0 || m.index >= len(m.filtered) {
		return tmuxActionItem{}, false
	}
	itemIndex := m.filtered[m.index]
	if itemIndex < 0 || itemIndex >= len(m.items) {
		return tmuxActionItem{}, false
	}
	return m.items[itemIndex], true
}

func runTmuxActions(args []string) error {
	sourcePane := ""
	positional := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		if args[i] == "--source-pane" && i+1 < len(args) {
			sourcePane = strings.TrimSpace(args[i+1])
			i++
			continue
		}
		positional = append(positional, args[i])
	}

	basePath := ""
	forcedAction := tmuxAction("")
	if len(positional) > 0 {
		if action := parseTmuxAction(positional[0]); action != "" {
			forcedAction = action
		} else {
			basePath = strings.TrimSpace(positional[0])
		}
	}
	if len(positional) > 1 && forcedAction == "" {
		forcedAction = parseTmuxAction(positional[1])
	}
	if basePath == "" {
		basePath = resolveTmuxActionsBasePath()
	}

	if forcedAction != "" {
		return runTmuxAction(basePath, sourcePane, forcedAction)
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
	return runTmuxAction(basePath, sourcePane, m.chosen)
}

func parseTmuxAction(value string) tmuxAction {
	switch strings.TrimSpace(strings.ToLower(value)) {
	case string(tmuxActionBack):
		return tmuxActionBack
	case string(tmuxActionShellSplit):
		return tmuxActionShellSplit
	case string(tmuxActionShellTab):
		return tmuxActionShellTab
	case string(tmuxActionIDE):
		return tmuxActionIDE
	case string(tmuxActionPR):
		return tmuxActionPR
	default:
		return ""
	}
}

func runTmuxAction(basePath string, sourcePane string, action tmuxAction) error {
	switch action {
	case tmuxActionBack:
		return returnToWTX(basePath, sourcePane)
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

func returnToWTX(basePath string, sourcePane string) error {
	basePath = strings.TrimSpace(basePath)
	if basePath == "" {
		return fmt.Errorf("missing path")
	}

	// Force unlock in "return to WTX" flows so stale pane ownership never blocks reuse.
	_ = runTmuxAgentExit([]string{"--worktree", basePath, "--code", "130", "--force-unlock"})

	paneID := strings.TrimSpace(sourcePane)
	if paneID == "" {
		paneID = strings.TrimSpace(os.Getenv("TMUX_PANE"))
	}
	if paneID == "" {
		if current, err := currentPaneID(); err == nil {
			paneID = strings.TrimSpace(current)
		}
	}
	if paneID == "" {
		return fmt.Errorf("unable to resolve active tmux pane")
	}

	bin := strings.TrimSpace(resolveAgentLifecycleBinary())
	if bin == "" {
		if discovered, err := exec.LookPath("wtx"); err == nil {
			bin = discovered
		} else {
			bin = "wtx"
		}
	}
	command := "exec " + shellQuote(bin)
	if err := exec.Command("tmux", "respawn-pane", "-k", "-c", basePath, "-t", paneID, command).Run(); err == nil {
		return nil
	}
	// Fallback to tmux "last active pane" target, which is reliable from popup contexts.
	return exec.Command("tmux", "respawn-pane", "-k", "-c", basePath, "-t", "!", command).Run()
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
	return fmt.Sprintf("%s tmux-actions", shellQuote(wtxBin))
}

func tmuxActionsCommandWithAction(wtxBin string, action tmuxAction) string {
	return fmt.Sprintf("%s tmux-actions %s", shellQuote(wtxBin), shellQuote(string(action)))
}

func resolveTmuxActionsBasePath() string {
	if path := strings.TrimSpace(os.Getenv("WTX_WORKTREE_PATH")); path != "" {
		return path
	}
	if out, err := exec.Command("tmux", "display-message", "-p", "#{@wtx_worktree_path}").Output(); err == nil {
		if path := strings.TrimSpace(string(out)); path != "" {
			return path
		}
	}
	if out, err := exec.Command("tmux", "display-message", "-p", "#{pane_current_path}").Output(); err == nil {
		if path := strings.TrimSpace(string(out)); path != "" {
			return path
		}
	}
	if sessionID, err := currentSessionID(); err == nil && strings.TrimSpace(sessionID) != "" {
		if out, err := exec.Command("tmux", "show-options", "-qv", "-t", sessionID, "@wtx_worktree_path").Output(); err == nil {
			if path := strings.TrimSpace(string(out)); path != "" {
				return path
			}
		}
		if out, err := exec.Command("tmux", "show-environment", "-t", sessionID, "WTX_WORKTREE_PATH").Output(); err == nil {
			line := strings.TrimSpace(string(out))
			if strings.HasPrefix(line, "WTX_WORKTREE_PATH=") {
				if path := strings.TrimSpace(strings.TrimPrefix(line, "WTX_WORKTREE_PATH=")); path != "" {
					return path
				}
			}
		}
	}
	cwd, err := os.Getwd()
	if err != nil {
		return ""
	}
	return cwd
}

var actionTokenSplitRe = regexp.MustCompile(`[^a-z0-9]+`)
