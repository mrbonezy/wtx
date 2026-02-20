package main

import (
	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/lipgloss"
)

type confirmKind int

const (
	confirmFieldKey = "confirm_result"

	confirmNone confirmKind = iota
	confirmDelete
	confirmUnlock
	confirmOpenDebugDelete
	confirmOpenDebugUnlock
	confirmOpenPickLocked
	confirmOpenBaseDefault
	confirmOpenFetchDefault
)

func wtxHuhTheme() *huh.Theme {
	t := *huh.ThemeCharm()
	t.Focused.FocusedButton = t.Focused.FocusedButton.Background(lipgloss.Color("#7D56F4"))
	t.Focused.Next = t.Focused.FocusedButton
	return &t
}

func newConfirmForm(title string, description string, result *bool) *huh.Form {
	confirm := huh.NewConfirm().
		Key(confirmFieldKey).
		Title(title).
		Description(description).
		Affirmative("Yes").
		Negative("No").
		Value(result)

	return huh.NewForm(huh.NewGroup(confirm)).
		WithTheme(wtxHuhTheme()).
		WithShowHelp(false)
}
