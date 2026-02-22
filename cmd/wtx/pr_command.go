package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/spf13/cobra"
)

const (
	prResolveTimeout        = 8 * time.Second
	prResolveSpinnerDelay   = 0 * time.Millisecond
	prResolveSpinnerMessage = "Resolving PR..."
)

func newPRCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "pr <number>",
		Short: "Select or create a branch worktree by pull request number",
		Long: "Resolves a pull request number to its head branch and then runs the same worktree flow as `wtx checkout`.\n\n" +
			"Requires `gh` and a GitHub-backed repository.",
		Example: strings.Join([]string{
			"  wtx pr 123",
		}, "\n"),
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) == 1 {
				return nil
			}
			if len(args) == 0 {
				return usageError(cmd, "missing pull request number")
			}
			return usageError(cmd, "too many arguments; provide exactly one pull request number")
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			number, err := parsePRNumber(args[0])
			if err != nil {
				return usageError(cmd, err.Error())
			}

			branch, err := resolvePRBranchWithSpinner(number)
			if err != nil {
				return err
			}
			return runCheckout(branch, false, "", nil, os.Args)
		},
	}
	return cmd
}

func parsePRNumber(raw string) (int, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return 0, errors.New("pull request number required")
	}
	n, err := strconv.Atoi(value)
	if err != nil || n <= 0 {
		return 0, fmt.Errorf("invalid pull request number %q", raw)
	}
	return n, nil
}

type ghPRBranchResult struct {
	HeadRefName string `json:"headRefName"`
	State       string `json:"state"`
}

func resolvePRBranchWithSpinner(number int) (string, error) {
	stop := startDelayedSpinner(prResolveSpinnerMessage, prResolveSpinnerDelay)
	defer stop()
	return resolvePRBranch(number)
}

func resolvePRBranch(number int) (string, error) {
	if number <= 0 {
		return "", errors.New("pull request number required")
	}
	_, repoRoot, err := requireGitContext("")
	if err != nil {
		return "", err
	}

	ghBin, err := exec.LookPath("gh")
	if err != nil {
		return "", errors.New("`gh` not installed; install GitHub CLI to use `wtx pr`")
	}
	ctx, cancel := context.WithTimeout(context.Background(), prResolveTimeout)
	defer cancel()
	cmd := exec.CommandContext(
		ctx,
		ghBin,
		"pr", "view", strconv.Itoa(number),
		"--json", "headRefName,state",
	)
	cmd.Dir = repoRoot
	out, err := cmd.CombinedOutput()
	if err != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return "", fmt.Errorf("resolving PR #%d timed out after %s", number, prResolveTimeout.Round(time.Second))
		}
		msg := strings.TrimSpace(string(out))
		if msg != "" {
			return "", fmt.Errorf("failed to resolve PR #%d: %s", number, msg)
		}
		return "", fmt.Errorf("failed to resolve PR #%d: %w", number, err)
	}
	var result ghPRBranchResult
	if err := json.Unmarshal(out, &result); err != nil {
		return "", fmt.Errorf("failed to parse PR #%d details: %w", number, err)
	}
	branch := strings.TrimSpace(result.HeadRefName)
	if branch == "" {
		return "", fmt.Errorf("PR #%d has no head branch", number)
	}
	return branch, nil
}

func startDelayedSpinner(message string, delay time.Duration) func() {
	if strings.TrimSpace(message) == "" {
		message = "Working..."
	}
	if delay < 0 {
		delay = 0
	}
	if !stderrIsTTY() {
		return func() {}
	}

	done := make(chan struct{})
	stopped := make(chan struct{})
	var once sync.Once
	go func() {
		defer close(stopped)
		timer := time.NewTimer(delay)
		defer timer.Stop()
		select {
		case <-done:
			return
		case <-timer.C:
		}

		s := newSpinner()
		frames := s.Spinner.Frames
		interval := s.Spinner.FPS
		if interval <= 0 {
			interval = 90 * time.Millisecond
		}
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		i := 0
		for {
			frame := frames[i%len(frames)]
			frame = s.Style.Render(frame)
			fmt.Fprintf(os.Stderr, "\r%s %s", frame, message)
			i++
			select {
			case <-done:
				fmt.Fprint(os.Stderr, "\r\033[2K")
				return
			case <-ticker.C:
			}
		}
	}()
	return func() {
		once.Do(func() {
			close(done)
			<-stopped
		})
	}
}

func stderrIsTTY() bool {
	info, err := os.Stderr.Stat()
	if err != nil {
		return false
	}
	return (info.Mode() & os.ModeCharDevice) != 0
}
