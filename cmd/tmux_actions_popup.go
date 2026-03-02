package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type tmuxAction string

var renameCurrentBranchTimeout = 3 * time.Second

const tmuxStatusRefreshTimeout = 500 * time.Millisecond

const (
	tmuxActionShellSplit  tmuxAction = "shell_split"
	tmuxActionShellTab    tmuxAction = "shell_tab"
	tmuxActionShellWindow tmuxAction = "shell_window"
	tmuxActionIDE         tmuxAction = "ide"
	tmuxActionPR          tmuxAction = "pr"
	tmuxActionBack        tmuxAction = "back_to_wtx"
	tmuxActionRename      tmuxAction = "rename_branch"
)

type tmuxActionItem struct {
	Alias       string
	Label       string
	Description string
	Keybinding  string
	Action      tmuxAction
	Disabled    bool
}

type tmuxActionsModel struct {
	basePath   string
	items      []tmuxActionItem
	filtered   []int
	index      int
	query      string
	chosen     tmuxAction
	cancel     bool
	updateHint string
	renameErr  string
	renameTo   string
}

func newTmuxActionsModel(basePath string, prAvailable bool, canOpenITermTab bool, canOpenShellWindow bool) tmuxActionsModel {
	terminalName := terminalProgramLabel()
	windowTerminalName := terminalWindowProgramLabel()
	items := []tmuxActionItem{
		{Alias: "back", Label: "Back to WTX", Description: "Back to WTX (stop agent)", Keybinding: "ctrl+w", Action: tmuxActionBack},
		{Alias: "ide", Label: "Open IDE", Description: "Open IDE", Keybinding: "ctrl+l", Action: tmuxActionIDE},
		{Alias: "pr", Label: "Open PR", Description: "Open PR", Keybinding: "ctrl+p", Action: tmuxActionPR, Disabled: !prAvailable},
		{Alias: "rename", Label: "Rename branch", Description: "Rename branch", Keybinding: "ctrl+r", Action: tmuxActionRename},
		{Alias: "shell", Label: "Open shell", Description: "Open shell (split down)", Keybinding: "ctrl+s", Action: tmuxActionShellSplit},
		{Alias: "tab", Label: fmt.Sprintf("Open shell tab (%s)", terminalName), Description: fmt.Sprintf("Open shell (new %s tab)", terminalName), Keybinding: "ctrl+t", Action: tmuxActionShellTab, Disabled: !canOpenITermTab},
		{Alias: "window", Label: fmt.Sprintf("Open shell window (%s)", windowTerminalName), Description: fmt.Sprintf("Open shell (new %s window)", windowTerminalName), Keybinding: "ctrl+n", Action: tmuxActionShellWindow, Disabled: !canOpenShellWindow},
	}
	sortTmuxActionItems(items)
	model := tmuxActionsModel{
		basePath: basePath,
		items:    items,
	}
	model.rebuildFiltered()
	return model
}

func (m tmuxActionsModel) Init() tea.Cmd {
	return checkInteractiveUpdateHintCmd()
}

func (m tmuxActionsModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case interactiveUpdateHintMsg:
		m.updateHint = strings.TrimSpace(msg.hint)
		return m, nil
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "esc":
			m.cancel = true
			return m, tea.Quit
		case "ctrl+b", "ctrl+w":
			return m.selectAction(tmuxActionBack)
		case "ctrl+s":
			return m.selectAction(tmuxActionShellSplit)
		case "ctrl+t":
			return m.selectAction(tmuxActionShellTab)
		case "ctrl+n":
			return m.selectAction(tmuxActionShellWindow)
		case "ctrl+l":
			return m.selectAction(tmuxActionIDE)
		case "ctrl+p":
			return m.selectAction(tmuxActionPR)
		case "ctrl+r":
			return m.selectAction(tmuxActionRename)
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
		case "up":
			if len(m.filtered) > 0 && m.index > 0 {
				m.index--
			}
			return m, nil
		case "down":
			if len(m.filtered) > 0 && m.index < len(m.filtered)-1 {
				m.index++
			}
			return m, nil
		case "enter":
			if action, ok := m.exactAliasAction(); ok {
				return m.selectAction(action)
			}
			selected, ok := m.selectedItem()
			if !ok {
				m.cancel = true
				return m, tea.Quit
			}
			if selected.Disabled {
				return m, nil
			}
			if selected.Action == tmuxActionRename {
				m.chosen = selected.Action
				return m, tea.Quit
			}
			m.chosen = selected.Action
			return m, tea.Quit
		default:
			if msg.Type == tea.KeyRunes {
				runes := msg.Runes
				if len(runes) == 0 {
					return m, nil
				}
				value := strings.ToLower(string(runes))
				if value == "/" && m.query == "" {
					return m, nil
				}
				m.query += value
				m.rebuildFiltered()
				return m, nil
			}
		}
	}
	return m, nil
}

func (m tmuxActionsModel) View() string {
	var b strings.Builder
	selectedStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#7D56F4"))
	normalStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("251"))
	disabledStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	dimStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("245"))

	queryLine := "/" + m.query
	if strings.TrimSpace(m.query) == "" {
		queryLine = "/command"
	}
	b.WriteString(dimStyle.Render(queryLine))
	b.WriteString("\n")
	b.WriteString(dimStyle.Render("────────────────────────────────────"))
	b.WriteString("\n")
	if len(m.filtered) == 0 {
		b.WriteString(disabledStyle.Render("No matching actions"))
		b.WriteString("\n")
	}
	for listIndex, itemIndex := range m.filtered {
		item := m.items[itemIndex]
		row := fmt.Sprintf("/%-8s %-32s %s", item.Alias, item.Description, item.Keybinding)
		if item.Disabled {
			row += " (unavailable)"
		}
		switch {
		case item.Disabled:
			b.WriteString(disabledStyle.Render(row))
		case listIndex == m.index:
			b.WriteString(selectedStyle.Render(row))
		default:
			b.WriteString(normalStyle.Render(row))
		}
		b.WriteString("\n")
	}
	b.WriteString("\n")
	b.WriteString(dimStyle.Render("enter run • ↑/↓ navigate • esc cancel"))
	if m.updateHint != "" {
		b.WriteString("\n")
		b.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("240")).Render(m.updateHint))
	}
	return b.String()
}

func (m tmuxActionsModel) selectAction(action tmuxAction) (tea.Model, tea.Cmd) {
	for _, item := range m.items {
		if item.Action != action {
			continue
		}
		if item.Disabled {
			return m, nil
		}
		if action == tmuxActionRename {
			m.chosen = action
			return m, tea.Quit
		}
		m.chosen = action
		return m, tea.Quit
	}
	return m, nil
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
	alias := strings.ToLower(strings.TrimSpace(item.Alias))
	return strings.Contains(alias, query)
}

func (m tmuxActionsModel) exactAliasAction() (tmuxAction, bool) {
	query := strings.TrimSpace(strings.ToLower(m.query))
	if query == "" {
		return "", false
	}
	for _, item := range m.items {
		if item.Disabled {
			continue
		}
		if strings.EqualFold(item.Alias, query) {
			return item.Action, true
		}
	}
	return "", false
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
	renameTo := ""
	positional := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		if args[i] == "--source-pane" && i+1 < len(args) {
			sourcePane = strings.TrimSpace(args[i+1])
			i++
			continue
		}
		if args[i] == "--rename-to" && i+1 < len(args) {
			renameTo = strings.TrimSpace(args[i+1])
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
	basePath = normalizeTmuxActionBasePathCandidate(basePath)
	if basePath == "" {
		basePath = resolveTmuxActionsBasePathFromPane(sourcePane)
	}
	if basePath == "" {
		basePath = resolveTmuxActionsBasePath()
	}
	if basePath == "" {
		return fmt.Errorf("missing worktree path in tmux session; run wtx from the target worktree to refresh WTX_WORKTREE_PATH")
	}

	if forcedAction != "" {
		return runTmuxAction(basePath, sourcePane, forcedAction, renameTo)
	}

	canOpenITermTab := canOpenShellInITermTab()
	canOpenWindow := canOpenShellWindow()
	prAvailable := hasCurrentPRFromStatusSummary(basePath)
	program := tea.NewProgram(newTmuxActionsModel(basePath, prAvailable, canOpenITermTab, canOpenWindow))
	finalModel, err := program.Run()
	if err != nil {
		return err
	}
	m := finalModel.(tmuxActionsModel)
	if m.cancel || m.chosen == "" {
		return nil
	}
	return runTmuxAction(basePath, sourcePane, m.chosen, m.renameTo)
}

func parseTmuxAction(value string) tmuxAction {
	switch strings.TrimSpace(strings.ToLower(value)) {
	case string(tmuxActionBack):
		return tmuxActionBack
	case string(tmuxActionShellSplit):
		return tmuxActionShellSplit
	case string(tmuxActionShellTab):
		return tmuxActionShellTab
	case string(tmuxActionShellWindow):
		return tmuxActionShellWindow
	case string(tmuxActionIDE):
		return tmuxActionIDE
	case string(tmuxActionPR):
		return tmuxActionPR
	case string(tmuxActionRename):
		return tmuxActionRename
	default:
		return ""
	}
}

func runTmuxAction(basePath string, sourcePane string, action tmuxAction, renameTo string) error {
	switch action {
	case tmuxActionBack:
		return returnToWTX(basePath, sourcePane)
	case tmuxActionShellSplit:
		cmd := exec.Command("tmux", "split-window", "-v", "-p", "50", "-c", basePath)
		return cmd.Run()
	case tmuxActionShellTab:
		return openShellInITermTab(basePath)
	case tmuxActionShellWindow:
		if isITermTerminal(resolveSessionParentTerminalProgram()) {
			return openShellInITermWindow(basePath)
		}
		return openShellInTerminalWindow(basePath)
	case tmuxActionIDE:
		clearPopupScreen()
		return runIDEPicker([]string{basePath})
	case tmuxActionPR:
		cmd := exec.Command("gh", "pr", "view", "--web")
		cmd.Dir = basePath
		out, err := cmd.CombinedOutput()
		if err != nil {
			msg := commandErrorMessage(err, out)
			if showTmuxActionErrorMessage(msg) {
				return nil
			}
			return fmt.Errorf("%s", msg)
		}
		return nil
	case tmuxActionRename:
		clearPopupScreen()
		if strings.TrimSpace(renameTo) != "" {
			return renameCurrentBranch(basePath, renameTo)
		}
		return runRenameBranchPopup(basePath)
	default:
		return nil
	}
}

func renameCurrentBranch(basePath string, renameTo string) error {
	basePath = strings.TrimSpace(basePath)
	if basePath == "" {
		return fmt.Errorf("missing path")
	}
	renameTo = strings.TrimSpace(renameTo)
	if renameTo == "" {
		return fmt.Errorf("branch name required")
	}
	timeout := renameCurrentBranchTimeout
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", "branch", "-m", renameTo)
	cmd.Dir = basePath
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	out, err := cmd.CombinedOutput()
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		msg := strings.TrimSpace(string(out))
		if msg != "" {
			return fmt.Errorf("git branch rename timed out after %s: %s", timeout, msg)
		}
		return fmt.Errorf("git branch rename timed out after %s", timeout)
	}
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if msg != "" {
			return fmt.Errorf("%s", msg)
		}
		return err
	}
	go refreshTmuxStatusNow()
	return nil
}

func runRenameBranchPopup(basePath string) error {
	errMsg := ""
	branch := ""
	for {
		model, err := tea.NewProgram(newRenameBranchModel(branch, errMsg)).Run()
		if err != nil {
			return err
		}
		m := model.(renameBranchModel)
		if m.cancelled {
			return nil
		}
		branch = strings.TrimSpace(m.branch)
		if branch == "" {
			errMsg = "Branch name required."
			continue
		}
		if err := renameCurrentBranch(basePath, branch); err != nil {
			errMsg = err.Error()
			continue
		}
		return nil
	}
}

type renameBranchModel struct {
	input     textinput.Model
	errMsg    string
	cancelled bool
	branch    string
}

func newRenameBranchModel(initialValue string, errMsg string) renameBranchModel {
	ti := textinput.New()
	ti.Placeholder = "new branch name"
	ti.Prompt = "> "
	ti.CharLimit = 256
	ti.Width = 50
	ti.SetValue(strings.TrimSpace(initialValue))
	ti.Focus()

	return renameBranchModel{
		input:  ti,
		errMsg: strings.TrimSpace(errMsg),
	}
}

func (m renameBranchModel) Init() tea.Cmd {
	return textinput.Blink
}

func (m renameBranchModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "esc":
			m.cancelled = true
			return m, tea.Quit
		case "enter":
			m.branch = strings.TrimSpace(m.input.Value())
			return m, tea.Quit
		}
	}

	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

func (m renameBranchModel) View() string {
	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#7D56F4"))
	errStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("203"))
	dimStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("245"))

	var b strings.Builder
	b.WriteString(titleStyle.Render("Rename branch to"))
	b.WriteString("\n")
	if m.errMsg != "" {
		b.WriteString(errStyle.Render(m.errMsg))
		b.WriteString("\n")
	}
	b.WriteString(m.input.View())
	b.WriteString("\n")
	b.WriteString(dimStyle.Render("enter submit • esc cancel"))
	return b.String()
}

func refreshTmuxStatusNow() {
	if _, err := exec.LookPath("tmux"); err != nil {
		return
	}

	queryWithTimeout := func(args ...string) (string, error) {
		ctx, cancel := context.WithTimeout(context.Background(), tmuxStatusRefreshTimeout)
		defer cancel()
		out, err := exec.CommandContext(ctx, "tmux", args...).Output()
		if err != nil {
			return "", err
		}
		return strings.TrimSpace(string(out)), nil
	}
	runWithTimeout := func(args ...string) error {
		ctx, cancel := context.WithTimeout(context.Background(), tmuxStatusRefreshTimeout)
		defer cancel()
		return exec.CommandContext(ctx, "tmux", args...).Run()
	}

	sessionID, err := queryWithTimeout("display-message", "-p", "#{session_id}")
	if err == nil && sessionID != "" {
		_ = runWithTimeout("refresh-client", "-S", "-t", sessionID)
		return
	}
	_ = runWithTimeout("refresh-client", "-S")
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

func hasCurrentPRFromStatusSummary(path string) bool {
	path = strings.TrimSpace(path)
	if path == "" {
		return false
	}
	branch := currentBranchInWorktree(path)
	if branch == "" {
		return false
	}
	summary := ghSummaryForBranchCached(path, branch)
	return prSummaryHasNumber(summary)
}

func prSummaryHasNumber(summary string) bool {
	return prSummaryLabelRe.MatchString(strings.TrimSpace(summary))
}

func canOpenShellInITermTab() bool {
	if iTermIntegrationDisabled() {
		return false
	}
	if !isITermTerminal(resolveSessionParentTerminalProgram()) {
		return false
	}
	return canControlITerm()
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

func openShellInITermWindow(path string) error {
	path = strings.TrimSpace(path)
	if path == "" {
		return fmt.Errorf("missing path")
	}
	script := `
on run argv
	set p to item 1 of argv
	tell application "iTerm"
		activate
		create window with default profile
		tell current session of current window
			write text "cd " & quoted form of p
		end tell
	end tell
end run
`
	cmd := exec.Command("osascript", "-e", script, "--", path)
	return cmd.Run()
}

func openShellInTerminalWindow(path string) error {
	path = strings.TrimSpace(path)
	if path == "" {
		return fmt.Errorf("missing path")
	}
	script := `
on run argv
	set p to item 1 of argv
	tell application "Terminal"
		activate
		do script "cd " & quoted form of p
	end tell
end run
`
	cmd := exec.Command("osascript", "-e", script, "--", path)
	return cmd.Run()
}

func clearPopupScreen() {
	fmt.Print("\x1b[2J\x1b[H")
}

func commandErrorMessage(err error, output []byte) string {
	if text := strings.TrimSpace(string(output)); text != "" {
		return text
	}
	if err != nil {
		return strings.TrimSpace(err.Error())
	}
	return "command failed"
}

func showTmuxActionErrorMessage(message string) bool {
	message = normalizeTmuxDisplayMessage(message)
	if message == "" || strings.TrimSpace(os.Getenv("TMUX")) == "" {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), tmuxStatusRefreshTimeout)
	defer cancel()
	return exec.CommandContext(ctx, "tmux", "display-message", "-d", "5000", message).Run() == nil
}

func normalizeTmuxDisplayMessage(message string) string {
	message = strings.TrimSpace(message)
	if message == "" {
		return ""
	}
	lines := strings.Fields(message)
	message = strings.Join(lines, " ")
	const maxLen = 220
	if len(message) <= maxLen {
		return message
	}
	return message[:maxLen-3] + "..."
}

func tmuxActionsPopupCommand(wtxBin string) string {
	return fmt.Sprintf("%s tmux-actions", shellQuote(wtxBin))
}

func tmuxActionsCommandWithAction(wtxBin string, action tmuxAction) string {
	return fmt.Sprintf("%s tmux-actions %s", shellQuote(wtxBin), shellQuote(string(action)))
}

func tmuxActionsCommandWithPathAndAction(wtxBin string, path string, action tmuxAction) string {
	return fmt.Sprintf("%s tmux-actions %s %s", shellQuote(wtxBin), shellQuote(path), shellQuote(string(action)))
}

func tmuxActionsCommandWithSourcePane(wtxBin string, sourcePane string, action tmuxAction) string {
	return fmt.Sprintf("%s tmux-actions --source-pane %s %s", shellQuote(wtxBin), shellQuote(sourcePane), shellQuote(string(action)))
}

func resolveTmuxActionsBasePathFromPane(sourcePane string) string {
	sourcePane = strings.TrimSpace(sourcePane)
	if sourcePane == "" {
		return ""
	}
	out, err := exec.Command("tmux", "display-message", "-p", "-t", sourcePane, "#{pane_current_path}").Output()
	if err != nil {
		return ""
	}
	return normalizeTmuxActionBasePathCandidate(string(out))
}

func resolveTmuxActionsBasePath() string {
	envPath := strings.TrimSpace(os.Getenv("WTX_WORKTREE_PATH"))
	optionPath := ""
	if out, err := exec.Command("tmux", "display-message", "-p", "#{@wtx_worktree_path}").Output(); err == nil {
		optionPath = strings.TrimSpace(string(out))
	}
	sessionOptionPath := ""
	sessionEnvPath := ""
	if sessionID, err := currentSessionID(); err == nil && strings.TrimSpace(sessionID) != "" {
		if out, err := exec.Command("tmux", "show-options", "-qv", "-t", sessionID, "@wtx_worktree_path").Output(); err == nil {
			sessionOptionPath = strings.TrimSpace(string(out))
		}
		if out, err := exec.Command("tmux", "show-environment", "-t", sessionID, "WTX_WORKTREE_PATH").Output(); err == nil {
			line := strings.TrimSpace(string(out))
			if strings.HasPrefix(line, "WTX_WORKTREE_PATH=") {
				sessionEnvPath = strings.TrimSpace(strings.TrimPrefix(line, "WTX_WORKTREE_PATH="))
			}
		}
	}
	return resolveTmuxActionsBasePathFromCandidates(envPath, optionPath, sessionOptionPath, sessionEnvPath)
}

func resolveTmuxActionsBasePathFromCandidates(paths ...string) string {
	for _, path := range paths {
		path = normalizeTmuxActionBasePathCandidate(path)
		if path != "" {
			return path
		}
	}
	return ""
}

func normalizeTmuxActionBasePathCandidate(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	if strings.Contains(path, "#{") {
		return ""
	}
	info, err := os.Stat(path)
	if err != nil || !info.IsDir() {
		return ""
	}
	return path
}

var prSummaryLabelRe = regexp.MustCompile(`\bPR\s+#\d+\b`)

func sortTmuxActionItems(items []tmuxActionItem) {
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].Disabled != items[j].Disabled {
			return !items[i].Disabled
		}
		return strings.ToLower(items[i].Alias) < strings.ToLower(items[j].Alias)
	})
}

func terminalProgramLabel() string {
	term := resolveSessionParentTerminalProgram()
	if isITermTerminal(term) {
		return "iTerm"
	}
	switch term {
	case "Apple_Terminal":
		return "Terminal"
	case "":
		return "terminal"
	default:
		return term
	}
}

func terminalWindowProgramLabel() string {
	if isITermTerminal(resolveSessionParentTerminalProgram()) {
		return "iTerm"
	}
	return "Terminal"
}

func resolveSessionParentTerminalProgram() string {
	if term := strings.TrimSpace(os.Getenv("WTX_PARENT_TERMINAL")); term != "" {
		return term
	}
	if sessionID, err := currentSessionID(); err == nil && strings.TrimSpace(sessionID) != "" {
		if out, err := exec.Command("tmux", "show-options", "-qv", "-t", sessionID, "@wtx_parent_terminal").Output(); err == nil {
			if term := strings.TrimSpace(string(out)); term != "" {
				return term
			}
		}
		if out, err := exec.Command("tmux", "show-environment", "-t", sessionID, "WTX_PARENT_TERMINAL").Output(); err == nil {
			line := strings.TrimSpace(string(out))
			if strings.HasPrefix(line, "WTX_PARENT_TERMINAL=") {
				if term := strings.TrimSpace(strings.TrimPrefix(line, "WTX_PARENT_TERMINAL=")); term != "" {
					return term
				}
			}
		}
	}
	if term := strings.TrimSpace(os.Getenv("TERM_PROGRAM")); term != "" {
		return term
	}
	if term := strings.TrimSpace(os.Getenv("TERM")); term != "" {
		return term
	}
	return "terminal"
}

func isITermTerminal(value string) bool {
	v := strings.ToLower(strings.TrimSpace(value))
	if v == "" {
		return false
	}
	return v == "iterm" || v == "iterm.app" || strings.Contains(v, "iterm")
}

func canControlITerm() bool {
	if _, err := exec.LookPath("osascript"); err != nil {
		return false
	}
	cmd := exec.Command("osascript", "-e", `tell application "iTerm" to version`)
	return cmd.Run() == nil
}

func canControlTerminal() bool {
	if _, err := exec.LookPath("osascript"); err != nil {
		return false
	}
	cmd := exec.Command("osascript", "-e", `tell application "Terminal" to version`)
	return cmd.Run() == nil
}

func canOpenShellWindow() bool {
	if isITermTerminal(resolveSessionParentTerminalProgram()) {
		return canOpenShellInITermTab()
	}
	return canControlTerminal()
}
