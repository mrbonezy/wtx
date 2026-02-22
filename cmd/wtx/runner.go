package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
)

type Runner struct {
	lockMgr *LockManager
}

func NewRunner(lockMgr *LockManager) *Runner {
	return &Runner{lockMgr: lockMgr}
}

type RunResult struct {
	Started bool
	Warning string
}

const loginShellCommand = "exec \"${SHELL:-/bin/sh}\" -l"

func (r *Runner) RunInWorktree(worktreePath string, branch string, lock *WorktreeLock) (RunResult, error) {
	worktreePath = strings.TrimSpace(worktreePath)
	if worktreePath == "" {
		return RunResult{}, errors.New("worktree path required")
	}
	branch = strings.TrimSpace(branch)

	cfg, err := LoadConfig()
	if err != nil {
		return RunResult{}, err
	}

	runCmd := strings.TrimSpace(cfg.AgentCommand)
	if runCmd == "" {
		runCmd = defaultAgentCommand
	}

	return r.runInWorktree(worktreePath, branch, lock, false, runCmd)
}

func (r *Runner) RunShellInWorktree(worktreePath string, branch string, lock *WorktreeLock) (RunResult, error) {
	return r.runInWorktree(worktreePath, branch, lock, true, "")
}

func (r *Runner) runInWorktree(worktreePath string, branch string, lock *WorktreeLock, openShell bool, runCmd string) (RunResult, error) {
	worktreePath = strings.TrimSpace(worktreePath)
	if worktreePath == "" {
		return RunResult{}, errors.New("worktree path required")
	}
	branch = strings.TrimSpace(branch)

	if tmuxAvailable() {
		return r.runInTmux(worktreePath, branch, lock, openShell, runCmd)
	}
	return r.runWithoutTmux(worktreePath, branch, lock, openShell, runCmd)
}

func (r *Runner) runInTmux(worktreePath string, branch string, lock *WorktreeLock, openShell bool, runCmd string) (RunResult, error) {
	paneID, _ := currentPaneID()
	newPaneID, err := splitCommandPane(worktreePath, commandToRunInTmux(worktreePath, openShell, runCmd))
	if err != nil {
		return RunResult{}, err
	}
	if !openShell {
		if err := r.lockWorktreeForPane(worktreePath, newPaneID, lock); err != nil {
			return RunResult{}, err
		}
	}
	activateWorktreeUI(worktreePath, branch)
	if newPaneID != "" {
		_ = exec.Command("tmux", "select-pane", "-t", newPaneID).Run()
	}
	if paneID != "" {
		if openShell {
			_ = exec.Command("tmux", "resize-pane", "-t", paneID, "-y", "1").Run()
		} else {
			_ = exec.Command("tmux", "kill-pane", "-t", paneID).Run()
		}
	}
	return RunResult{Started: true}, nil
}

func (r *Runner) runWithoutTmux(worktreePath string, branch string, lock *WorktreeLock, openShell bool, runCmd string) (RunResult, error) {
	cmd := shellCommand(worktreePath, commandToRun(openShell, runCmd))
	if err := cmd.Start(); err != nil {
		return RunResult{}, err
	}
	if !openShell {
		boundLock, err := r.lockWorktreeForPID(worktreePath, cmd.Process.Pid, lock)
		if err != nil {
			_ = cmd.Process.Kill()
			_, _ = cmd.Process.Wait()
			return RunResult{}, err
		}
		if boundLock != nil {
			defer boundLock.Release()
		}
	}

	activateWorktreeUI(worktreePath, branch)

	runErr := cmd.Wait()
	result := RunResult{Started: true, Warning: "tmux unavailable; running in current terminal"}
	if runErr != nil {
		return result, fmt.Errorf("worktree command failed: %w", runErr)
	}
	return result, nil
}

func shellCommand(worktreePath string, runCmd string) *exec.Cmd {
	cmd := exec.Command("/bin/sh", "-lc", runCmd)
	cmd.Dir = worktreePath
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd
}

func commandToRun(openShell bool, runCmd string) string {
	if openShell {
		return loginShellCommand
	}
	return runCmd
}

func commandToRunInTmux(worktreePath string, openShell bool, runCmd string) string {
	if openShell {
		return loginShellCommand
	}
	bin := strings.TrimSpace(resolveAgentLifecycleBinary())
	if bin == "" {
		return runCmd + "; exec \"${SHELL:-/bin/sh}\" -l"
	}
	startCmd := shellQuote(bin) + " tmux-agent-start --worktree " + shellQuote(worktreePath)
	exitCmd := shellQuote(bin) + " tmux-agent-exit --worktree " + shellQuote(worktreePath)
	return startCmd + "; " +
		"finish(){ code=\"$1\"; " + exitCmd + " --code \"$code\"; exec \"${SHELL:-/bin/sh}\" -l; }; " +
		"trap 'finish 130' INT TERM; " +
		runCmd + "; code=$?; trap - INT TERM; finish \"$code\""
}

func activateWorktreeUI(worktreePath string, branch string) {
	recordRecentBranchForWorktree(worktreePath, branch)
	if tmuxAvailable() {
		// Avoid full-screen clears in tmux when swapping panes; this noticeably reduces flicker.
		setDynamicWorktreeStatus(worktreePath)
		// Avoid overriding tmux-managed dynamic titles with a static branch title.
		setITermWTXTab()
		return
	}
	clearScreen()
	setITermWTXBranchTab(branch)
}

func (r *Runner) lockWorktreeForPane(worktreePath string, paneID string, existingLock *WorktreeLock) error {
	if strings.TrimSpace(paneID) == "" {
		return nil
	}
	pid, err := panePID(paneID)
	if err != nil {
		return err
	}
	_, err = r.lockWorktreeForPID(worktreePath, pid, existingLock)
	return err
}

func (r *Runner) lockWorktreeForPID(worktreePath string, pid int, existingLock *WorktreeLock) (*WorktreeLock, error) {
	if existingLock != nil {
		return existingLock, existingLock.RebindPID(pid)
	}
	_, repoRoot, err := requireGitContext(worktreePath)
	if err != nil {
		return nil, err
	}
	return r.lockMgr.AcquireForPID(repoRoot, worktreePath, pid)
}

func (r *Runner) OpenURL(url string) error {
	url = strings.TrimSpace(url)
	if url == "" {
		return errors.New("no PR URL for selected worktree")
	}
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	return cmd.Start()
}
