package main

import (
	"os"
	"strings"
)

func envFlagEnabled(name string) bool {
	value := strings.ToLower(strings.TrimSpace(os.Getenv(name)))
	switch value {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

func tmuxIntegrationDisabled() bool {
	return envFlagEnabled("WTX_DISABLE_TMUX")
}

func iTermIntegrationDisabled() bool {
	return envFlagEnabled("WTX_DISABLE_ITERM")
}

func testModeEnabled() bool {
	return envFlagEnabled("WTX_TEST_MODE")
}
