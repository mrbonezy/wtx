package main

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

func ensureFreshTmuxSession(args []string) (bool, error) {
	if strings.TrimSpace(os.Getenv("TMUX")) != "" {
		return false, nil
	}
	if _, err := exec.LookPath("tmux"); err != nil {
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
	tmuxArgs := []string{"new-session", "-e", "WTX_STATUS_BIN=" + bin, "-s", session, "-c", cwd, bin}
	if len(args) > 1 {
		tmuxArgs = append(tmuxArgs, args[1:]...)
	}
	cmd := exec.Command("tmux", tmuxArgs...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return false, err
	}
	return true, nil
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
	if strings.TrimSpace(banner) == "" {
		return
	}
	banner = stripANSI(banner)
	sessionID, err := currentSessionID()
	if err != nil || strings.TrimSpace(sessionID) == "" {
		return
	}
	configureTmuxStatus(sessionID, "200", tmuxStatusIntervalSeconds)
	tmuxSetOption(sessionID, "status-left", " "+banner+" ")
}

func setDynamicWorktreeStatus(worktreePath string) {
	worktreePath = strings.TrimSpace(worktreePath)
	if worktreePath == "" {
		return
	}
	sessionID, err := currentSessionID()
	if err != nil || strings.TrimSpace(sessionID) == "" {
		return
	}
	bin := resolveStatusCommandBinary()
	if strings.TrimSpace(bin) == "" {
		return
	}
	cmd := "#(" + shellQuote(bin) + " tmux-status --worktree " + shellQuote(worktreePath) + ")"
	configureTmuxStatus(sessionID, "300", tmuxStatusIntervalSeconds)
	tmuxSetOption(sessionID, "status-left", " "+cmd+" ")
	tmuxSetOption(sessionID, "status-right", " ⌥← → panes | ^⇧↑↓ resize | ^S shell | ^A ide | ^P pr ")
	tmuxSetOption(sessionID, "status-right-length", "64")
	titleCmd := "#(" + shellQuote(bin) + " tmux-title --worktree " + shellQuote(worktreePath) + ")"
	tmuxSetOption(sessionID, "set-titles", "on")
	tmuxSetOption(sessionID, "set-titles-string", titleCmd)
}

func clearScreen() {
	_ = exec.Command("tmux", "clear-history").Run()
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
	sessionID, err := currentSessionID()
	if err != nil || strings.TrimSpace(sessionID) == "" {
		return
	}
	// Ensure session dies when terminal client closes, so pane-backed locks do not linger.
	tmuxSetOption(sessionID, "destroy-unattached", "on")
	// Disable mouse so normal terminal copy (Cmd+C) works.
	tmuxSetOption(sessionID, "mouse", "off")
	// Bind Option+arrow keys for quick pane navigation.
	_ = exec.Command("tmux", "bind-key", "-n", "M-Up", "select-pane", "-U").Run()
	_ = exec.Command("tmux", "bind-key", "-n", "M-Down", "select-pane", "-D").Run()
	_ = exec.Command("tmux", "bind-key", "-n", "M-Left", "select-pane", "-L").Run()
	_ = exec.Command("tmux", "bind-key", "-n", "M-Right", "select-pane", "-R").Run()
	// Bind Ctrl+Shift+Up/Down for quick vertical pane resizing (avoids common macOS Ctrl+Arrow shortcuts).
	_ = exec.Command("tmux", "bind-key", "-r", "-n", "C-S-Up", "resize-pane", "-U", "3").Run()
	_ = exec.Command("tmux", "bind-key", "-r", "-n", "C-S-Down", "resize-pane", "-D", "3").Run()
	// Preserve modified key chords (for example Shift+Enter in coding agents) inside wtx-managed tmux sessions.
	tmuxSetWindowOption(sessionID, "xterm-keys", "on")
	tmuxSetGlobalWindowOption("xterm-keys", "on")
	tmuxSetServerOption("extended-keys", "always")
	tmuxSetServerOption("extended-keys-format", "csi-u")
	tmuxAppendServerOption("terminal-features", ",*:extkeys")
	tmuxAppendGlobalOption("terminal-features", ",*:extkeys")

	// Bind ctrl+s to split and open shell in current pane's directory
	// Bind ctrl+a to open IDE picker popup
	// Bind ctrl+p to open PR for current branch in browser
	wtxBin := resolveAgentLifecycleBinary()
	if strings.TrimSpace(wtxBin) != "" {
		// Use split-window directly for shell (faster, no need to resolve path)
		_ = exec.Command("tmux", "bind-key", "-n", "C-s", "split-window", "-v", "-p", "50", "-c", "#{pane_current_path}").Run()
		// For IDE, use popup with directory picker TUI
		idePickerCmd := fmt.Sprintf("%s ide-picker #{pane_current_path}", strings.ReplaceAll(wtxBin, "'", "'\\''"))
		_ = exec.Command("tmux", "bind-key", "-n", "C-a", "popup", "-E", "-w", "60", "-h", "20", idePickerCmd).Run()
		// For PR, use gh pr view --web directly
		_ = exec.Command("tmux", "bind-key", "-n", "C-p", "run-shell", "-b", "cd #{pane_current_path} && gh pr view --web").Run()
	}
}

func configureTmuxStatus(sessionID string, leftLength string, interval string) {
	tmuxSetOption(sessionID, "status", "1")
	tmuxSetOption(sessionID, "status-position", "bottom")
	tmuxSetOption(sessionID, "status-justify", "left")
	tmuxSetOption(sessionID, "status-style", "fg=#d0d0d0,bg=#3d2a5c")
	tmuxSetOption(sessionID, "status-left-length", leftLength)
	tmuxSetOption(sessionID, "status-right", "")
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
	if _, repoRoot, err := requireGitContext(worktreePath); err == nil && strings.TrimSpace(repoRoot) != "" {
		_ = NewLockManager().ReleaseIfOwned(repoRoot, worktreePath)
	}
	return writeTmuxAgentState(worktreePath, tmuxAgentState{
		State:        "exited",
		ExitCode:     exitCode,
		ExitedAtUnix: time.Now().Unix(),
	})
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
	gitPath, err := gitPath()
	if err != nil {
		return "", err
	}
	repoRoot, err := repoRootForDir(worktreePath, gitPath)
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
