package main

import (
	"context"
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

type interactiveUpdateHintMsg struct {
	hint    string
	isError bool
}

func checkInteractiveUpdateHintCmd() tea.Cmd {
	return func() tea.Msg {
		cur := strings.TrimSpace(currentVersion())
		ctx, cancel := context.WithTimeout(context.Background(), startupUpdateTimeout)
		defer cancel()

		result, err := checkForUpdatesWithThrottle(ctx, cur, defaultUpdateInterval)
		hint, isError := formatInteractiveUpdateHint(cur, result, err)
		return interactiveUpdateHintMsg{hint: hint, isError: isError}
	}
}

func formatInteractiveUpdateHint(current string, result updateCheckResult, err error) (string, bool) {
	current = strings.TrimSpace(current)
	if current == "" {
		current = "unknown"
	}
	if err != nil {
		return fmt.Sprintf("wtx update check failed: %v", err), true
	}
	if strings.TrimSpace(result.ResolveError) != "" {
		return fmt.Sprintf("wtx update check failed: %s", strings.TrimSpace(result.ResolveError)), true
	}
	if err == nil && result.UpdateAvailable {
		return fmt.Sprintf(wtxUpdateCommandFormat, result.CurrentVersion, result.LatestVersion), false
	}
	return fmt.Sprintf("wtx %s", current), false
}
