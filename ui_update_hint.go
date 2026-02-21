package main

import (
	"context"
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
)

type interactiveUpdateHintMsg struct {
	hint string
}

func checkInteractiveUpdateHintCmd() tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), startupUpdateTimeout)
		defer cancel()

		result, err := checkForUpdatesWithThrottle(ctx, currentVersion(), defaultUpdateInterval)
		if err != nil || !result.UpdateAvailable {
			return interactiveUpdateHintMsg{}
		}
		return interactiveUpdateHintMsg{
			hint: fmt.Sprintf(wtxUpdateCommandFormat, result.CurrentVersion, result.LatestVersion),
		}
	}
}
