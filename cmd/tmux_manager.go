package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
)

const tmuxStatusIntervalSeconds = "10"
const tmuxStatusRightHint = " ^A actions | ^W back#{?#{>:#{window_panes},1}, | ⌥⇧↑/⌥⇧↓ resize,} "

func ensureFreshTmuxSession(args []string) (bool, error) {
	if tmuxIntegrationDisabled() {
		return false, nil
	}
	if _, err := exec.LookPath("tmux"); err != nil {
		return false, nil
	}
	inTmux := strings.TrimSpace(os.Getenv("TMUX")) != ""
	if inTmux && !shouldStartIsolatedTmuxSession(resolveCurrentTerminalProgram(), resolveCurrentSessionParentTerminalProgram()) {
		return false, nil
	}

	bin, err := resolveSelfBinary(args)
	if err != nil {
		return false, err
	}

	cwd, err := os.Getwd()
	if err != nil {
		return false, err
	}

	setITermWTXTab()

	session := fmt.Sprintf("wtx-%d", time.Now().UnixNano())
	parentTerminal := resolveCurrentTerminalProgram()
	tmuxArgs := []string{
		"new-session", "-d",
		"-e", "WTX_STATUS_BIN=" + bin,
		"-e", "WTX_PARENT_TERMINAL=" + parentTerminal,
		"-s", session, "-c", cwd,
	}
	if configDir := strings.TrimSpace(os.Getenv(configDirOverrideEnv)); configDir != "" {
		tmuxArgs = append(tmuxArgs, "-e", configDirOverrideEnv+"="+configDir)
	}
	cmd := exec.Command("tmux", tmuxArgs...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if msg != "" {
			return false, fmt.Errorf("tmux new-session failed: %s", msg)
		}
		return false, err
	}

	applyStartupThemeToSession(session, cwd, parentTerminal)
	if inTmux {
		// Apply defaults before launching the pane command so modified-key handling is
		// active from the first prompt in the new session.
		applyWTXSessionDefaults(session, false)
		if err := launchCommandInSession(session, bin, args[1:]); err != nil {
			return false, err
		}
		if err := exec.Command("tmux", "switch-client", "-t", session).Run(); err != nil {
			return false, err
		}
		// Re-apply after client switch so mouse/table bindings are set in attached context.
		applyWTXSessionDefaults(session, false)
		return true, nil
	}

	launchToken := fmt.Sprintf("wtx-launch-%d", time.Now().UnixNano())
	if err := launchCommandInSessionAfterSignal(session, bin, args[1:], launchToken); err != nil {
		return false, err
	}

	go func() {
		applyWTXSessionDefaults(session, false)
		_ = tmuxSignal(launchToken)
	}()

	attach := exec.Command("tmux", "attach-session", "-t", session)
	attach.Stdin = os.Stdin
	attach.Stdout = os.Stdout
	attach.Stderr = os.Stderr
	if err := attach.Run(); err != nil {
		return false, err
	}
	return true, nil
}

func applyStartupThemeToSession(sessionID string, cwd string, parentTerminal string) {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return
	}
	cwd = strings.TrimSpace(cwd)
	banner := stripANSI(renderBanner("", cwd, ""))
	// Session is detached at startup; avoid destroy-unattached here.
	applyWTXSessionDefaults(sessionID, false)
	if cwd != "" {
		_ = exec.Command("tmux", "set-environment", "-t", sessionID, "WTX_WORKTREE_PATH", cwd).Run()
		tmuxSetOption(sessionID, "@wtx_worktree_path", cwd)
	}
	parentTerminal = strings.TrimSpace(parentTerminal)
	if parentTerminal != "" {
		_ = exec.Command("tmux", "set-environment", "-t", sessionID, "WTX_PARENT_TERMINAL", parentTerminal).Run()
		tmuxSetOption(sessionID, "@wtx_parent_terminal", parentTerminal)
	}
	configureTmuxStatus(sessionID, "200", tmuxStatusIntervalSeconds)
	tmuxSetOption(sessionID, "status-left", " "+banner+" ")
}

func launchCommandInSession(sessionID string, bin string, args []string) error {
	sessionID = strings.TrimSpace(sessionID)
	bin = strings.TrimSpace(bin)
	if sessionID == "" || bin == "" {
		return fmt.Errorf("session and binary are required")
	}
	command := shellQuote(bin)
	for _, arg := range args {
		command += " " + shellQuote(arg)
	}
	// Run directly in the pane so the command isn't visibly typed into the shell.
	return exec.Command("tmux", "respawn-pane", "-k", "-t", sessionID+":0.0", command).Run()
}

func launchCommandInSessionAfterSignal(sessionID string, bin string, args []string, signal string) error {
	sessionID = strings.TrimSpace(sessionID)
	bin = strings.TrimSpace(bin)
	signal = strings.TrimSpace(signal)
	if sessionID == "" || bin == "" || signal == "" {
		return fmt.Errorf("session, binary, and signal are required")
	}
	command := shellQuote(bin)
	for _, arg := range args {
		command += " " + shellQuote(arg)
	}
	waitCommand := "tmux wait-for " + shellQuote(signal) + "; exec " + command
	return exec.Command("tmux", "respawn-pane", "-k", "-t", sessionID+":0.0", "/bin/sh", "-lc", waitCommand).Run()
}

func tmuxSignal(signal string) error {
	signal = strings.TrimSpace(signal)
	if signal == "" {
		return nil
	}
	return exec.Command("tmux", "wait-for", "-S", signal).Run()
}

func resolveSelfBinary(args []string) (string, error) {
	arg0 := strings.TrimSpace(args[0])
	if arg0 == "" {
		return "", fmt.Errorf("unable to resolve executable path")
	}
	if filepath.IsAbs(arg0) {
		return arg0, nil
	}
	if strings.Contains(arg0, string(os.PathSeparator)) {
		abs, err := filepath.Abs(arg0)
		if err != nil {
			return "", err
		}
		return abs, nil
	}
	path, err := exec.LookPath(arg0)
	if err != nil {
		return "", err
	}
	return path, nil
}

func setStartupStatusBanner() {
	if tmuxIntegrationDisabled() {
		return
	}
	if strings.TrimSpace(os.Getenv("TMUX")) == "" {
		return
	}
	ensureWTXSessionDefaults()
	cwd, err := os.Getwd()
	if err != nil {
		return
	}
	setStatusBanner(renderBanner("", cwd, ""))
}

func splitCommandPane(worktreePath string, runCmd string) (string, error) {
	cmd := exec.Command("tmux", "split-window", "-v", "-p", "70", "-d", "-c", worktreePath, "-P", "-F", "#{pane_id}", "/bin/sh", "-lc", runCmd)
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

func tmuxAvailable() bool {
	if tmuxIntegrationDisabled() {
		return false
	}
	if strings.TrimSpace(os.Getenv("TMUX")) == "" {
		return false
	}
	_, err := exec.LookPath("tmux")
	return err == nil
}

func currentPaneID() (string, error) {
	out, err := exec.Command("tmux", "display-message", "-p", "#{pane_id}").Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

func panePID(paneID string) (int, error) {
	out, err := exec.Command("tmux", "display-message", "-p", "-t", paneID, "#{pane_pid}").Output()
	if err != nil {
		return 0, err
	}
	value := strings.TrimSpace(string(out))
	if value == "" {
		return 0, fmt.Errorf("tmux pane pid not found")
	}
	pid, err := strconv.Atoi(value)
	if err != nil {
		return 0, err
	}
	return pid, nil
}

func currentSessionID() (string, error) {
	out, err := exec.Command("tmux", "display-message", "-p", "#{session_id}").Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

func currentWindowID() (string, error) {
	out, err := exec.Command("tmux", "display-message", "-p", "#{window_id}").Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

func renderBanner(branch string, path string, ghSummary string) string {
	label := "WTX"
	if branch != "" {
		label = label + "  " + branch
	}
	if path != "" {
		label = label + "  " + path
	}
	if strings.TrimSpace(ghSummary) != "" {
		label = label + "  " + strings.TrimSpace(ghSummary)
	}
	style := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("#FFF7DB")).
		Background(lipgloss.Color("#7D56F4")).
		Padding(0, 1)
	return style.Render(label)
}

func setStatusBanner(banner string) {
	if tmuxIntegrationDisabled() {
		return
	}
	if strings.TrimSpace(banner) == "" {
		return
	}
	banner = stripANSI(banner)
	sessionID, err := currentSessionID()
	if err != nil || strings.TrimSpace(sessionID) == "" {
		return
	}
	ensureWTXSessionDefaults()
	configureTmuxStatus(sessionID, "200", tmuxStatusIntervalSeconds)
	tmuxSetOption(sessionID, "status-left", " "+banner+" ")
}

func setDynamicWorktreeStatus(worktreePath string) {
	if tmuxIntegrationDisabled() {
		return
	}
	worktreePath = strings.TrimSpace(worktreePath)
	if worktreePath == "" {
		return
	}
	sessionID, err := currentSessionID()
	if err != nil || strings.TrimSpace(sessionID) == "" {
		return
	}
	ensureWTXSessionDefaults()
	bin := resolveStatusCommandBinary()
	if strings.TrimSpace(bin) == "" {
		return
	}
	cmd := "#(" + shellQuote(bin) + " tmux-status --worktree " + shellQuote(worktreePath) + ")"
	configureTmuxStatus(sessionID, "300", tmuxStatusIntervalSeconds)
	_ = exec.Command("tmux", "set-environment", "-t", sessionID, "WTX_WORKTREE_PATH", worktreePath).Run()
	tmuxSetOption(sessionID, "@wtx_worktree_path", worktreePath)
	tmuxSetOption(sessionID, "status-left", " "+cmd+" ")
	tmuxSetOption(sessionID, "status-right", " ^A actions | ^S split | ^P PR | ^L IDE#{?#{>:#{window_panes},1}, | ⌥↑/⌥↓ move | ⌥⇧↑/⌥⇧↓ resize,} ")
	tmuxSetOption(sessionID, "status-right-length", "132")
	titleCmd := "#(" + shellQuote(bin) + " tmux-title --worktree " + shellQuote(worktreePath) + ")"
	tmuxSetOption(sessionID, "set-titles", "on")
	tmuxSetOption(sessionID, "set-titles-string", titleCmd)
	configureTmuxActionBindings(sessionID, resolveAgentLifecycleBinary())
}

func clearScreen() {
	if tmuxAvailable() {
		_ = exec.Command("tmux", "clear-history").Run()
	}
	fmt.Fprint(os.Stdout, "\x1b[2J\x1b[H")
}

func stripANSI(value string) string {
	out := make([]rune, 0, len(value))
	inEscape := false
	for _, r := range value {
		if inEscape {
			if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
				inEscape = false
			}
			continue
		}
		if r == '\x1b' {
			inEscape = true
			continue
		}
		out = append(out, r)
	}
	return string(out)
}

func shellQuote(value string) string {
	if value == "" {
		return "''"
	}
	return "'" + strings.ReplaceAll(value, "'", `'\''`) + "'"
}

func ensureWTXSessionDefaults() {
	if tmuxIntegrationDisabled() {
		return
	}
	sessionID, err := currentSessionID()
	if err != nil || strings.TrimSpace(sessionID) == "" {
		return
	}
	applyWTXSessionDefaults(sessionID, true)
}

func applyWTXSessionDefaults(sessionID string, enableDestroyUnattached bool) {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return
	}
	// Ensure session dies when terminal client closes, so pane-backed locks do not linger.
	if enableDestroyUnattached {
		tmuxSetOption(sessionID, "destroy-unattached", "on")
	}
	terminalProgram := resolveCurrentTerminalProgram()
	if !shouldDisableTmuxInputEnhancements(terminalProgram) {
		// Keep wheel scrolling and mouse interactions working across modern terminals.
		tmuxSetOption(sessionID, "mouse", "on")
	}
	// Style split panes so inactive panes are visually de-emphasized and separators are clearer.
	for _, option := range wtxPaneStyleOptions() {
		tmuxSetWindowOption(sessionID, option.key, option.value)
	}
	keyTable := tmuxSessionKeyTable(sessionID)
	tmuxSetOption(sessionID, "key-table", keyTable)
	configureTmuxStatusRefreshHooks(sessionID)
	configureTmuxPaneBadgeBehavior(sessionID)
	configureTmuxMouseBindings(keyTable)
	clearLegacyTmuxInputBindings(keyTable)

	// Only resize when split panes are present.
	_ = exec.Command("tmux", "bind-key", "-r", "-T", keyTable, "M-Up", "if-shell", "-F", "#{>:#{window_panes},1}", "select-pane -U").Run()
	_ = exec.Command("tmux", "bind-key", "-r", "-T", keyTable, "M-Down", "if-shell", "-F", "#{>:#{window_panes},1}", "select-pane -D").Run()
	_ = exec.Command("tmux", "bind-key", "-r", "-T", keyTable, "M-S-Up", "if-shell", "-F", "#{>:#{window_panes},1}", "resize-pane -U 3").Run()
	_ = exec.Command("tmux", "bind-key", "-r", "-T", keyTable, "M-S-Down", "if-shell", "-F", "#{>:#{window_panes},1}", "resize-pane -D 3").Run()
	if !shouldDisableTmuxInputEnhancements(terminalProgram) {
		// Preserve modified key chords (for example Shift+Enter in coding agents) inside wtx-managed tmux sessions.
		tmuxSetWindowOption(sessionID, "xterm-keys", "on")
		tmuxSetGlobalWindowOption("xterm-keys", "on")
		tmuxSetServerOption("extended-keys", "always")
		tmuxSetServerOption("extended-keys-format", "csi-u")
		tmuxAppendServerOption("terminal-features", ",*:extkeys")
		tmuxAppendGlobalOption("terminal-features", ",*:extkeys")
	}

	configureTmuxActionBindings(sessionID, resolveAgentLifecycleBinary())
}

func clearLegacyTmuxInputBindings(keyTable string) {
	tables := []string{"root", "copy-mode", "copy-mode-vi"}
	if keyTable = strings.TrimSpace(keyTable); keyTable != "" {
		tables = append(tables, keyTable)
	}
	for _, table := range tables {
		_ = exec.Command("tmux", "unbind-key", "-T", table, "M-[").Run()
	}
}

func configureTmuxMouseBindings(keyTable string) {
	keyTable = strings.TrimSpace(keyTable)
	if keyTable == "" {
		return
	}
	// Custom key tables do not inherit root/copy-mode bindings. Bind all relevant tables so wheel
	// behavior is consistent even during table transitions.
	for _, table := range []string{keyTable, "root", "copy-mode", "copy-mode-vi"} {
		for _, binding := range tmuxMouseBindings(table) {
			args := make([]string, 0, len(binding.args)+5)
			args = append(args, "bind-key")
			if binding.repeatable {
				args = append(args, "-r")
			}
			args = append(args, "-T", table, binding.key)
			args = append(args, binding.args...)
			_ = exec.Command("tmux", args...).Run()
		}
	}
}

type tmuxBinding struct {
	key        string
	args       []string
	repeatable bool
}

func tmuxMouseBindings(table string) []tmuxBinding {
	table = strings.TrimSpace(table)
	if table == "copy-mode" || table == "copy-mode-vi" {
		return []tmuxBinding{
			{key: "WheelUpPane", args: []string{"select-pane -t=; send-keys -X -N 1 scroll-up"}, repeatable: true},
			{key: "WheelDownPane", args: []string{"select-pane -t=; send-keys -X -N 1 scroll-down"}, repeatable: true},
		}
	}
	return []tmuxBinding{
		{key: "MouseDown1Pane", args: []string{"select-pane", "-t="}},
		{key: "MouseDown1Border", args: []string{"select-pane", "-t="}},
		{key: "MouseDrag1Border", args: []string{"resize-pane", "-M"}},
		{
			key: "WheelUpPane",
			args: []string{
				"if-shell", "-F", "-t", "=", "#{||:#{alternate_on},#{mouse_any_flag}}",
				"send-keys -M",
				"select-pane -t=; copy-mode -e; send-keys -X -N 1 scroll-up",
			},
			repeatable: true,
		},
		{
			key: "WheelDownPane",
			args: []string{
				"if-shell", "-F", "-t", "=", "#{||:#{alternate_on},#{mouse_any_flag}}",
				"send-keys -M",
				"select-pane -t=; copy-mode -e; send-keys -X -N 1 scroll-down",
			},
			repeatable: true,
		},
	}
}

type tmuxOption struct {
	key   string
	value string
}

func wtxPaneStyleOptions() []tmuxOption {
	return []tmuxOption{
		{key: "pane-border-style", value: "fg=#1e1530"},
		{key: "pane-active-border-style", value: "fg=#6a4b9c"},
		{key: "window-style", value: "fg=#ddd7f2,bg=#1f1a2c"},
		{key: "window-active-style", value: "fg=#8f89a3,bg=#14111c"},
		{key: "mode-style", value: "fg=#1e1530,bg=#6a4b9c"},
		{key: "pane-border-lines", value: "heavy"},
		{key: "pane-border-status", value: "off"},
		{key: "pane-border-format", value: "#{?#{&&:#{pane_active},#{>:#{window_panes},1}},#[bold fg=#1e1530 bg=#6a4b9c] ACTIVE #[default],}"},
	}
}

func configureTmuxPaneBadgeBehavior(sessionID string) {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return
	}
	updateCmd := `if -F "#{>:#{window_panes},1}" "set-window-option -q -t . pane-border-status top" "set-window-option -q -t . pane-border-status off"`
	_ = exec.Command("tmux", "set-hook", "-t", sessionID, "after-split-window", updateCmd).Run()
	_ = exec.Command("tmux", "set-hook", "-t", sessionID, "after-kill-pane", updateCmd).Run()
	_ = exec.Command("tmux", "set-hook", "-t", sessionID, "after-join-pane", updateCmd).Run()
	_ = exec.Command("tmux", "set-hook", "-t", sessionID, "after-break-pane", updateCmd).Run()
	for _, windowID := range tmuxSessionWindowIDs(sessionID) {
		_ = exec.Command("tmux", "if-shell", "-F", "-t", windowID, "#{>:#{window_panes},1}", "set-window-option -q -t "+windowID+" pane-border-status top", "set-window-option -q -t "+windowID+" pane-border-status off").Run()
	}
}

func tmuxSessionWindowIDs(sessionID string) []string {
	out, err := exec.Command("tmux", "list-windows", "-t", sessionID, "-F", "#{window_id}").Output()
	if err != nil {
		return nil
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	ids := make([]string, 0, len(lines))
	for _, line := range lines {
		id := strings.TrimSpace(line)
		if id == "" {
			continue
		}
		ids = append(ids, id)
	}
	return ids
}

func configureTmuxActionBindings(sessionID string, wtxBin string) {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return
	}
	if strings.TrimSpace(wtxBin) == "" {
		return
	}
	keyTable := tmuxSessionKeyTable(sessionID)
	actionsPopupCmd := tmuxActionsPopupCommand(wtxBin)
	splitCmd := tmuxActionsCommandWithPathAndAction(wtxBin, "#{pane_current_path}", tmuxActionShellSplit)
	prCmd := tmuxActionsCommandWithPathAndAction(wtxBin, "#{pane_current_path}", tmuxActionPR)
	ideCmd := tmuxActionsCommandWithSourcePane(wtxBin, "#{pane_id}", tmuxActionIDE)
	backCmd := tmuxActionsCommandWithSourcePane(wtxBin, "#{pane_id}", tmuxActionBack)

	_ = exec.Command("tmux", "bind-key", "-T", keyTable, "C-a", "popup", "-E", "-d", "#{pane_current_path}", "-w", "72", "-h", "20", actionsPopupCmd+" --source-pane '#{pane_id}'").Run()
	_ = exec.Command("tmux", "bind-key", "-T", keyTable, "C-s", "run-shell", "-b", splitCmd).Run()
	_ = exec.Command("tmux", "bind-key", "-T", keyTable, "C-p", "run-shell", "-b", prCmd).Run()
	_ = exec.Command("tmux", "bind-key", "-T", keyTable, "C-l", "popup", "-E", "-d", "#{pane_current_path}", "-w", "60", "-h", "20", ideCmd).Run()
	_ = exec.Command("tmux", "bind-key", "-T", keyTable, "C-w", "run-shell", "-b", backCmd).Run()
}

func tmuxSessionKeyTable(sessionID string) string {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return "root"
	}
	replacer := strings.NewReplacer("$", "s", "@", "w", "%", "p", ":", "_", ".", "_", "-", "_")
	return "wtx_" + replacer.Replace(sessionID)
}

func configureTmuxStatusRefreshHooks(sessionID string) {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return
	}
	refreshCmd := "refresh-client -S"
	hooks := []string{
		"after-split-window",
		"after-kill-pane",
		"after-new-window",
		"after-kill-window",
		"after-rename-window",
		"client-session-changed",
	}
	for _, hook := range hooks {
		_ = exec.Command("tmux", "set-hook", "-t", sessionID, hook, refreshCmd).Run()
	}
}

func configureTmuxStatus(sessionID string, leftLength string, interval string) {
	tmuxSetOption(sessionID, "status", "1")
	tmuxSetOption(sessionID, "status-position", "bottom")
	tmuxSetOption(sessionID, "status-justify", "left")
	tmuxSetOption(sessionID, "status-style", "fg=#d0d0d0,bg=#3d2a5c")
	tmuxSetOption(sessionID, "status-left-length", leftLength)
	tmuxSetOption(sessionID, "status-right", tmuxStatusRightHint)
	tmuxSetOption(sessionID, "status-right-length", "64")
	interval = strings.TrimSpace(interval)
	if interval == "" {
		interval = tmuxStatusIntervalSeconds
	}
	tmuxSetOption(sessionID, "status-interval", interval)
}

func tmuxSetOption(sessionID string, key string, value string) {
	if strings.TrimSpace(sessionID) == "" {
		return
	}
	_ = exec.Command("tmux", "set-option", "-q", "-t", sessionID, key, value).Run()
}

func tmuxSetWindowOption(sessionID string, key string, value string) {
	if strings.TrimSpace(sessionID) == "" {
		return
	}
	_ = exec.Command("tmux", "set-window-option", "-q", "-t", sessionID, key, value).Run()
}

func tmuxSetServerOption(key string, value string) {
	if strings.TrimSpace(key) == "" {
		return
	}
	_ = exec.Command("tmux", "set-option", "-s", "-q", key, value).Run()
}

func tmuxSetGlobalWindowOption(key string, value string) {
	if strings.TrimSpace(key) == "" {
		return
	}
	_ = exec.Command("tmux", "set-window-option", "-g", "-q", key, value).Run()
}

func tmuxAppendServerOption(key string, value string) {
	if strings.TrimSpace(key) == "" || strings.TrimSpace(value) == "" {
		return
	}
	_ = exec.Command("tmux", "set-option", "-s", "-as", "-q", key, value).Run()
}

func tmuxAppendGlobalOption(key string, value string) {
	if strings.TrimSpace(key) == "" || strings.TrimSpace(value) == "" {
		return
	}
	_ = exec.Command("tmux", "set-option", "-g", "-as", "-q", key, value).Run()
}

func tmuxBindKey(sessionID string, key string, command string) {
	if strings.TrimSpace(sessionID) == "" || strings.TrimSpace(key) == "" || strings.TrimSpace(command) == "" {
		return
	}
	_ = exec.Command("tmux", "bind-key", "-t", sessionID, key, "run-shell", command).Run()
}

func tmuxBindKeyGlobal(key string, command string) {
	if strings.TrimSpace(key) == "" || strings.TrimSpace(command) == "" {
		return
	}
	_ = exec.Command("tmux", "bind-key", "-n", key, "run-shell", command).Run()
}

func resolveStatusCommandBinary() string {
	if p, err := exec.LookPath("wtx"); err == nil && strings.TrimSpace(p) != "" && fileLooksExecutable(p) {
		return p
	}
	if v := strings.TrimSpace(os.Getenv("WTX_STATUS_BIN")); v != "" {
		if fileLooksExecutable(v) {
			return v
		}
	}
	if p, err := os.Executable(); err == nil && strings.TrimSpace(p) != "" && fileLooksExecutable(p) {
		return p
	}
	if p, err := exec.LookPath("wtx"); err == nil && strings.TrimSpace(p) != "" && fileLooksExecutable(p) {
		return p
	}
	return ""
}

func resolveAgentLifecycleBinary() string {
	candidates := make([]string, 0, 3)
	if p, err := os.Executable(); err == nil && strings.TrimSpace(p) != "" && fileLooksExecutable(p) {
		candidates = append(candidates, p)
	}
	if v := strings.TrimSpace(os.Getenv("WTX_STATUS_BIN")); v != "" && fileLooksExecutable(v) {
		candidates = append(candidates, v)
	}
	if p, err := exec.LookPath("wtx"); err == nil && strings.TrimSpace(p) != "" && fileLooksExecutable(p) {
		candidates = append(candidates, p)
	}
	for _, candidate := range candidates {
		if supportsTmuxAgentLifecycle(candidate) {
			return candidate
		}
	}
	return ""
}

func supportsTmuxAgentLifecycle(bin string) bool {
	bin = strings.TrimSpace(bin)
	if bin == "" {
		return false
	}
	cmd := exec.Command(bin, "tmux-agent-start")
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	return cmd.Run() == nil
}

func resolveCurrentTerminalProgram() string {
	term := strings.TrimSpace(os.Getenv("TERM_PROGRAM"))
	if term != "" {
		return term
	}
	term = strings.TrimSpace(os.Getenv("TERM"))
	if term != "" {
		return term
	}
	return "terminal"
}

func resolveCurrentSessionParentTerminalProgram() string {
	sessionID, err := currentSessionID()
	if err != nil || strings.TrimSpace(sessionID) == "" {
		return ""
	}
	if out, err := exec.Command("tmux", "show-options", "-qv", "-t", sessionID, "@wtx_parent_terminal").Output(); err == nil {
		if term := strings.TrimSpace(string(out)); term != "" {
			return term
		}
	}
	if out, err := exec.Command("tmux", "show-environment", "-t", sessionID, "WTX_PARENT_TERMINAL").Output(); err == nil {
		line := strings.TrimSpace(string(out))
		if strings.HasPrefix(line, "WTX_PARENT_TERMINAL=") {
			return strings.TrimSpace(strings.TrimPrefix(line, "WTX_PARENT_TERMINAL="))
		}
	}
	return ""
}

func shouldStartIsolatedTmuxSession(currentTerminal string, sessionParentTerminal string) bool {
	current := normalizeTerminalProgram(currentTerminal)
	sessionParent := normalizeTerminalProgram(sessionParentTerminal)
	if current == "" || sessionParent == "" {
		return false
	}
	return current != sessionParent
}

func normalizeTerminalProgram(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func shouldDisableTmuxInputEnhancements(terminalProgram string) bool {
	term := normalizeTerminalProgram(terminalProgram)
	if strings.Contains(term, "iterm") {
		return true
	}
	return strings.Contains(term, "ghostty")
}

type tmuxAgentState struct {
	State        string `json:"state"`
	ExitCode     int    `json:"exit_code"`
	ExitedAtUnix int64  `json:"exited_at_unix"`
}

func runTmuxAgentStart(args []string) error {
	worktreePath := parseWorktreeArg(args)
	if strings.TrimSpace(worktreePath) == "" {
		return nil
	}
	return writeTmuxAgentState(worktreePath, tmuxAgentState{
		State:        "running",
		ExitCode:     0,
		ExitedAtUnix: 0,
	})
}

func runTmuxAgentExit(args []string) error {
	worktreePath := parseWorktreeArg(args)
	if strings.TrimSpace(worktreePath) == "" {
		return nil
	}
	exitCode := parseIntArg(args, "--code", 0)
	forceUnlock := parseBoolArg(args, "--force-unlock")
	if _, repoRoot, err := requireGitContext(worktreePath); err == nil && strings.TrimSpace(repoRoot) != "" {
		lockMgr := NewLockManager()
		_ = lockMgr.ReleaseIfOwned(repoRoot, worktreePath)
		if forceUnlock {
			_ = lockMgr.ForceUnlock(repoRoot, worktreePath)
		}
	}
	return writeTmuxAgentState(worktreePath, tmuxAgentState{
		State:        "exited",
		ExitCode:     exitCode,
		ExitedAtUnix: time.Now().Unix(),
	})
}

func parseBoolArg(args []string, key string) bool {
	for i := 0; i < len(args); i++ {
		if strings.TrimSpace(args[i]) == key {
			return true
		}
	}
	return false
}

func parseIntArg(args []string, key string, fallback int) int {
	for i := 0; i < len(args); i++ {
		if args[i] != key || i+1 >= len(args) {
			continue
		}
		value := strings.TrimSpace(args[i+1])
		if value == "" {
			return fallback
		}
		parsed, err := strconv.Atoi(value)
		if err != nil {
			return fallback
		}
		return parsed
	}
	return fallback
}

func tmuxAgentSummary(worktreePath string) string {
	state, ok := readTmuxAgentState(worktreePath)
	if !ok {
		return ""
	}
	if strings.EqualFold(strings.TrimSpace(state.State), "exited") {
		return "Agent exited (" + strconv.Itoa(state.ExitCode) + ")"
	}
	return ""
}

func readTmuxAgentState(worktreePath string) (tmuxAgentState, bool) {
	path, err := tmuxAgentStatePath(worktreePath)
	if err != nil {
		return tmuxAgentState{}, false
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return tmuxAgentState{}, false
	}
	var state tmuxAgentState
	if err := json.Unmarshal(data, &state); err != nil {
		return tmuxAgentState{}, false
	}
	return state, true
}

func writeTmuxAgentState(worktreePath string, state tmuxAgentState) error {
	path, err := tmuxAgentStatePath(worktreePath)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	payload, err := json.Marshal(state)
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, payload, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func tmuxAgentStatePath(worktreePath string) (string, error) {
	worktreePath = strings.TrimSpace(worktreePath)
	if worktreePath == "" {
		return "", os.ErrInvalid
	}
	repoRoot, err := repoRootForDir(worktreePath, "git")
	if err != nil {
		return "", err
	}
	worktreeID, err := worktreeID(repoRoot, worktreePath)
	if err != nil {
		return "", err
	}
	home := strings.TrimSpace(os.Getenv("HOME"))
	if home == "" {
		return "", os.ErrNotExist
	}
	return filepath.Join(home, ".wtx", "agent-state", worktreeID+".json"), nil
}

func fileLooksExecutable(path string) bool {
	path = strings.TrimSpace(path)
	if path == "" {
		return false
	}
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return false
	}
	return info.Mode()&0o111 != 0
}
