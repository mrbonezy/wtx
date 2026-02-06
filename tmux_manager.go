package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
)

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
	configureTmuxStatus(sessionID, "200", "")
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
	configureTmuxStatus(sessionID, "300", "10")
	tmuxSetOption(sessionID, "status-left", " "+cmd+" ")
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
}

func configureTmuxStatus(sessionID string, leftLength string, interval string) {
	tmuxSetOption(sessionID, "status", "1")
	tmuxSetOption(sessionID, "status-position", "bottom")
	tmuxSetOption(sessionID, "status-justify", "left")
	tmuxSetOption(sessionID, "status-style", "fg=#FFF7DB,bg=#7D56F4")
	tmuxSetOption(sessionID, "status-left-length", leftLength)
	tmuxSetOption(sessionID, "status-right", "")
	if strings.TrimSpace(interval) != "" {
		tmuxSetOption(sessionID, "status-interval", interval)
	}
}

func tmuxSetOption(sessionID string, key string, value string) {
	if strings.TrimSpace(sessionID) == "" {
		return
	}
	_ = exec.Command("tmux", "set-option", "-t", sessionID, key, value).Run()
}

func resolveStatusCommandBinary() string {
	if v := strings.TrimSpace(os.Getenv("WTX_STATUS_BIN")); v != "" {
		if fileLooksExecutable(v) {
			return v
		}
	}
	if p, err := exec.LookPath("wtx"); err == nil && strings.TrimSpace(p) != "" {
		return p
	}
	if p, err := os.Executable(); err == nil && strings.TrimSpace(p) != "" && fileLooksExecutable(p) {
		return p
	}
	return ""
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
