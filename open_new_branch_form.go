package main

import (
	"errors"
	"strings"

	"github.com/charmbracelet/huh"
)

const (
	openNewBranchNameKey = "open_new_branch_name"
	openNewBaseRefKey    = "open_new_base_ref"
	openNewFetchKey      = "open_new_fetch"
)

func newOpenNewBranchForm(branch *string, baseRef *string, fetch *bool) *huh.Form {
	branchInput := huh.NewInput().
		Key(openNewBranchNameKey).
		Title("Branch name").
		Inline(true).
		Value(branch).
		Validate(func(value string) error {
			if strings.TrimSpace(value) == "" {
				return errors.New("branch name is required")
			}
			return nil
		})

	baseInput := huh.NewInput().
		Key(openNewBaseRefKey).
		Title("Checkout from").
		Inline(true).
		Value(baseRef)

	fetchConfirm := huh.NewConfirm().
		Key(openNewFetchKey).
		Title("Fetch before checkout?").
		Affirmative("Yes").
		Negative("No").
		Inline(true).
		Value(fetch)

	return huh.NewForm(
		huh.NewGroup(branchInput, baseInput, fetchConfirm),
	).
		WithTheme(wtxHuhTheme()).
		WithShowHelp(false)
}
