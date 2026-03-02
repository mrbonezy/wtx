package cmd

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

func TestActionMatchesQuery_Substring(t *testing.T) {
	item := tmuxActionItem{
		Alias:  "shell",
		Action: tmuxActionShellSplit,
	}
	if !actionMatchesQuery(item, "ell") {
		t.Fatalf("expected substring query to match")
	}
}

func TestActionMatchesQuery_AliasPrefix(t *testing.T) {
	item := tmuxActionItem{
		Alias:  "rename",
		Action: tmuxActionRename,
	}
	if !actionMatchesQuery(item, "re") {
		t.Fatalf("expected alias prefix query to match")
	}
}

func TestActionMatchesQuery_DoesNotMatchNonAliasText(t *testing.T) {
	item := tmuxActionItem{
		Alias:       "back",
		Label:       "Back to WTX",
		Description: "Back to WTX (stop agent)",
		Action:      tmuxActionBack,
	}
	if actionMatchesQuery(item, "return") {
		t.Fatalf("expected non-alias query not to match")
	}
}

func TestTmuxActionsModel_RebuildFiltered(t *testing.T) {
	m := newTmuxActionsModel("/tmp", true, false, false)
	m.query = "pr"
	m.rebuildFiltered()
	item, ok := m.selectedItem()
	if !ok {
		t.Fatalf("expected a selected item after filtering")
	}
	if item.Action != tmuxActionPR {
		t.Fatalf("expected PR action, got %q", item.Action)
	}
}

func TestParseTmuxAction_BackToWTX(t *testing.T) {
	got := parseTmuxAction("back_to_wtx")
	if got != tmuxActionBack {
		t.Fatalf("expected back_to_wtx action, got %q", got)
	}
}

func TestParseTmuxAction_RenameBranch(t *testing.T) {
	got := parseTmuxAction("rename_branch")
	if got != tmuxActionRename {
		t.Fatalf("expected rename_branch action, got %q", got)
	}
}

func TestParseTmuxAction_ShellWindow(t *testing.T) {
	got := parseTmuxAction("shell_window")
	if got != tmuxActionShellWindow {
		t.Fatalf("expected shell_window action, got %q", got)
	}
}

func TestTmuxActionsModel_CtrlBSelectsBack(t *testing.T) {
	m := newTmuxActionsModel("/tmp", true, false, false)
	updatedModel, _ := m.Update(tea.KeyMsg{Type: tea.KeyCtrlB})
	updated := updatedModel.(tmuxActionsModel)
	if updated.chosen != tmuxActionBack {
		t.Fatalf("expected ctrl+b to choose back action, got %q", updated.chosen)
	}
}

func TestTmuxActionsModel_CtrlRSelectsRename(t *testing.T) {
	m := newTmuxActionsModel("/tmp", true, false, false)
	updatedModel, _ := m.Update(tea.KeyMsg{Type: tea.KeyCtrlR})
	updated := updatedModel.(tmuxActionsModel)
	if updated.chosen != tmuxActionRename {
		t.Fatalf("expected ctrl+r to choose rename action, got %q", updated.chosen)
	}
}

func TestTmuxActionsModel_ShowsShellWindowActionDisabledWhenUnavailable(t *testing.T) {
	m := newTmuxActionsModel("/tmp", true, false, false)
	found := false
	for _, item := range m.items {
		if item.Action != tmuxActionShellWindow {
			continue
		}
		found = true
		if !item.Disabled {
			t.Fatalf("expected shell window action to be disabled when unavailable")
		}
	}
	if !found {
		t.Fatalf("expected shell window action to exist")
	}
}

func TestTmuxActionsModel_ShowsShellTabActionDisabledWhenUnavailable(t *testing.T) {
	m := newTmuxActionsModel("/tmp", true, false, false)
	found := false
	for _, item := range m.items {
		if item.Action != tmuxActionShellTab {
			continue
		}
		found = true
		if !item.Disabled {
			t.Fatalf("expected shell tab action to be disabled when unavailable")
		}
	}
	if !found {
		t.Fatalf("expected shell tab action to exist")
	}
}

func TestTmuxActionsModel_ViewShowsShortcutHints(t *testing.T) {
	m := newTmuxActionsModel("/tmp", true, false, false)
	view := m.View()
	if !strings.Contains(view, "/back") {
		t.Fatalf("expected /back alias in view, got %q", view)
	}
	if !strings.Contains(view, "ctrl+w") {
		t.Fatalf("expected ctrl+w hint in view rows, got %q", view)
	}
	if !strings.Contains(view, "ctrl+r") {
		t.Fatalf("expected ctrl+r hint in view rows, got %q", view)
	}
	if !strings.Contains(view, "enter run • ↑/↓ navigate • esc cancel") {
		t.Fatalf("expected minimal footer hint, got %q", view)
	}
}

func TestTmuxActionsModel_LockedSlashAndBackspace(t *testing.T) {
	m := newTmuxActionsModel("/tmp", true, false, false)

	updatedModel, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	updated := updatedModel.(tmuxActionsModel)
	if updated.query != "" {
		t.Fatalf("expected leading slash rune to be ignored, got %q", updated.query)
	}

	updatedModel, _ = updated.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'p', 'r'}})
	updated = updatedModel.(tmuxActionsModel)
	if updated.query != "pr" {
		t.Fatalf("expected query to be pr, got %q", updated.query)
	}

	updatedModel, _ = updated.Update(tea.KeyMsg{Type: tea.KeyBackspace})
	updated = updatedModel.(tmuxActionsModel)
	if updated.query != "p" {
		t.Fatalf("expected query to backspace to p, got %q", updated.query)
	}

	updatedModel, _ = updated.Update(tea.KeyMsg{Type: tea.KeyBackspace})
	updated = updatedModel.(tmuxActionsModel)
	if updated.query != "" {
		t.Fatalf("expected query to backspace to empty, got %q", updated.query)
	}

	updatedModel, _ = updated.Update(tea.KeyMsg{Type: tea.KeyBackspace})
	updated = updatedModel.(tmuxActionsModel)
	if updated.query != "" {
		t.Fatalf("expected backspace on empty query to do nothing, got %q", updated.query)
	}
}

func TestTmuxActionsModel_AllowsTypingKAndJIntoQuery(t *testing.T) {
	m := newTmuxActionsModel("/tmp", true, false, false)

	updatedModel, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'k'}})
	updated := updatedModel.(tmuxActionsModel)
	if updated.query != "k" {
		t.Fatalf("expected query to include k, got %q", updated.query)
	}

	updatedModel, _ = updated.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	updated = updatedModel.(tmuxActionsModel)
	if updated.query != "kj" {
		t.Fatalf("expected query to include j, got %q", updated.query)
	}
}

func TestTmuxActionsModel_EnterExecutesExactAlias(t *testing.T) {
	m := newTmuxActionsModel("/tmp", true, false, false)
	m.query = "rename"
	m.rebuildFiltered()

	updatedModel, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	updated := updatedModel.(tmuxActionsModel)
	if updated.chosen != tmuxActionRename {
		t.Fatalf("expected exact alias to choose rename, got %q", updated.chosen)
	}
}

func TestTmuxActionsModel_ViewRemovesSearchAndCaret(t *testing.T) {
	m := newTmuxActionsModel("/tmp", true, false, false)
	view := m.View()

	if strings.Contains(view, "Actions") {
		t.Fatalf("did not expect Actions title in view, got %q", view)
	}
	if strings.Contains(view, "Search:") {
		t.Fatalf("did not expect Search label in view, got %q", view)
	}
	if strings.Contains(view, "\n> ") {
		t.Fatalf("did not expect caret glyph row prefix in view, got %q", view)
	}
	if !strings.Contains(view, "/command") {
		t.Fatalf("expected slash input placeholder in view, got %q", view)
	}
}

func TestTmuxActionsModel_ViewRendersAliasDescriptionAndKeybinding(t *testing.T) {
	m := newTmuxActionsModel("/tmp", true, false, false)
	view := m.View()

	if !strings.Contains(view, "/ide") {
		t.Fatalf("expected alias column to include /ide, got %q", view)
	}
	if !strings.Contains(view, "Open IDE") {
		t.Fatalf("expected description column to include Open IDE, got %q", view)
	}
	if !strings.Contains(view, "ctrl+l") {
		t.Fatalf("expected keybinding column to include ctrl+l, got %q", view)
	}
}

func TestSortTmuxActionItems_UnavailableLastThenAlias(t *testing.T) {
	items := []tmuxActionItem{
		{Alias: "zeta", Disabled: false},
		{Alias: "beta", Disabled: true},
		{Alias: "alpha", Disabled: false},
		{Alias: "gamma", Disabled: true},
	}
	sortTmuxActionItems(items)

	got := []string{
		items[0].Alias,
		items[1].Alias,
		items[2].Alias,
		items[3].Alias,
	}
	want := []string{"alpha", "zeta", "beta", "gamma"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("expected sorted aliases %v, got %v", want, got)
		}
	}
}

func TestPRSummaryHasNumber(t *testing.T) {
	if !prSummaryHasNumber("PR #12 | CI ok 3/3 | GH mergeable | Review 1/1 u:0") {
		t.Fatalf("expected PR summary with number to match")
	}
	if prSummaryHasNumber("PR - | CI - | GH - | Review -") {
		t.Fatalf("expected empty PR summary to not match")
	}
}

func TestNormalizeTmuxDisplayMessage(t *testing.T) {
	got := normalizeTmuxDisplayMessage("  no pull requests found\nfor branch \"x\"  ")
	if got != `no pull requests found for branch "x"` {
		t.Fatalf("unexpected normalized message: %q", got)
	}
}

func TestTmuxActionsCommandWithAction_InjectsSourcePane(t *testing.T) {
	got := tmuxActionsCommandWithAction("/usr/local/bin/wtx", tmuxActionBack)
	if strings.Contains(got, "--source-pane") {
		t.Fatalf("did not expect source-pane flag in %q", got)
	}
	if want := "back_to_wtx"; !strings.Contains(got, want) {
		t.Fatalf("expected back action token %q in %q", want, got)
	}
}


func TestTmuxActionsCommandWithSourcePane(t *testing.T) {
	got := tmuxActionsCommandWithSourcePane("/usr/local/bin/wtx", "%12", tmuxActionIDE)
	if want := "--source-pane"; !strings.Contains(got, want) {
		t.Fatalf("expected %q in %q", want, got)
	}
	if want := "'%12'"; !strings.Contains(got, want) {
		t.Fatalf("expected quoted pane id %q in %q", want, got)
	}
	if want := "ide"; !strings.Contains(got, want) {
		t.Fatalf("expected action %q in %q", want, got)
	}
}

func TestTmuxActionsCommandWithPathAndAction(t *testing.T) {
	got := tmuxActionsCommandWithPathAndAction("/usr/local/bin/wtx", "/tmp/repo path", tmuxActionRename)
	if want := "tmux-actions"; !strings.Contains(got, want) {
		t.Fatalf("expected %q in %q", want, got)
	}
	if want := "'/tmp/repo path'"; !strings.Contains(got, want) {
		t.Fatalf("expected quoted path %q in %q", want, got)
	}
	if want := "rename_branch"; !strings.Contains(got, want) {
		t.Fatalf("expected action %q in %q", want, got)
	}
}

func TestRenameCurrentBranch_Succeeds(t *testing.T) {
	repo := initRenameTestRepo(t)
	runGitInRepo(t, repo, "checkout", "-b", "before-rename")

	if err := renameCurrentBranch(repo, "after-rename"); err != nil {
		t.Fatalf("renameCurrentBranch failed: %v", err)
	}

	head := strings.TrimSpace(runGitOutput(t, repo, "rev-parse", "--abbrev-ref", "HEAD"))
	if head != "after-rename" {
		t.Fatalf("expected HEAD to be after-rename, got %q", head)
	}
}

func TestRenameCurrentBranch_TargetAlreadyExists(t *testing.T) {
	repo := initRenameTestRepo(t)
	runGitInRepo(t, repo, "checkout", "-b", "before-rename")
	runGitInRepo(t, repo, "checkout", "-b", "existing")
	runGitInRepo(t, repo, "checkout", "before-rename")

	err := renameCurrentBranch(repo, "existing")
	if err == nil {
		t.Fatalf("expected rename error when target exists")
	}
	if !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("expected clear exists error, got %v", err)
	}
}

func TestRenameCurrentBranch_TimesOut(t *testing.T) {
	repo := initRenameTestRepo(t)
	runGitInRepo(t, repo, "checkout", "-b", "before-rename")

	prev := renameCurrentBranchTimeout
	renameCurrentBranchTimeout = 100 * time.Millisecond
	t.Cleanup(func() {
		renameCurrentBranchTimeout = prev
	})

	fakeBinDir := t.TempDir()
	gitName := "git"
	script := "#!/bin/sh\nsleep 1\n"
	if runtime.GOOS == "windows" {
		gitName = "git.bat"
		script = "@echo off\r\nping -n 2 127.0.0.1 >NUL\r\n"
	}
	fakeGitPath := filepath.Join(fakeBinDir, gitName)
	if err := os.WriteFile(fakeGitPath, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake git: %v", err)
	}
	t.Setenv("PATH", fakeBinDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	start := time.Now()
	err := renameCurrentBranch(repo, "after-rename")
	if err == nil {
		t.Fatalf("expected timeout error")
	}
	if !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("expected timeout message, got %v", err)
	}
	if elapsed := time.Since(start); elapsed > 3*time.Second {
		t.Fatalf("expected fail-fast timeout; took %s", elapsed)
	}
}

func initRenameTestRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	runGitInRepo(t, dir, "init")
	runGitInRepo(t, dir, "config", "user.name", "Test User")
	runGitInRepo(t, dir, "config", "user.email", "test@example.com")

	readmePath := filepath.Join(dir, "README.md")
	if err := os.WriteFile(readmePath, []byte("seed\n"), 0o644); err != nil {
		t.Fatalf("write README: %v", err)
	}
	runGitInRepo(t, dir, "add", "README.md")
	runGitInRepo(t, dir, "commit", "-m", "seed")
	return dir
}

func runGitInRepo(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, strings.TrimSpace(string(out)))
	}
}

func runGitOutput(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, strings.TrimSpace(string(out)))
	}
	return string(out)
}

func TestResolveTmuxActionsBasePathFromCandidates(t *testing.T) {
	optionPath := t.TempDir()
	sessionOptionPath := t.TempDir()
	sessionEnvPath := t.TempDir()
	cwdPath := t.TempDir()

	t.Run("uses first non-empty candidate", func(t *testing.T) {
		got := resolveTmuxActionsBasePathFromCandidates(
			"",
			optionPath,
			sessionOptionPath,
			sessionEnvPath,
			cwdPath,
		)
		if got != optionPath {
			t.Fatalf("expected %s, got %q", optionPath, got)
		}
	})

	t.Run("falls back through session metadata then cwd", func(t *testing.T) {
		got := resolveTmuxActionsBasePathFromCandidates(
			"",
			"",
			"",
			sessionEnvPath,
			cwdPath,
		)
		if got != sessionEnvPath {
			t.Fatalf("expected %s, got %q", sessionEnvPath, got)
		}

		got = resolveTmuxActionsBasePathFromCandidates("", "", "", "", cwdPath)
		if got != cwdPath {
			t.Fatalf("expected %s, got %q", cwdPath, got)
		}
	})

	t.Run("skips tmux format placeholders and invalid directories", func(t *testing.T) {
		got := resolveTmuxActionsBasePathFromCandidates("#{pane_current_path}", filepath.Join(t.TempDir(), "missing"), optionPath)
		if got != optionPath {
			t.Fatalf("expected fallback to %s, got %q", optionPath, got)
		}
	})

	t.Run("returns empty when no valid tmux metadata path exists", func(t *testing.T) {
		got := resolveTmuxActionsBasePathFromCandidates("", "#{pane_current_path}", filepath.Join(t.TempDir(), "missing"), "")
		if got != "" {
			t.Fatalf("expected empty path, got %q", got)
		}
	})
}
