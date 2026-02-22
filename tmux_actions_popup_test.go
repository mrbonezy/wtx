package main

import (
	"strings"
	"testing"
)

func TestActionMatchesQuery_Substring(t *testing.T) {
	item := tmuxActionItem{
		Label:    "Open shell (split down)",
		Action:   tmuxActionShellSplit,
		Keywords: "shell split pane",
	}
	if !actionMatchesQuery(item, "split") {
		t.Fatalf("expected substring query to match")
	}
}

func TestActionMatchesQuery_TokenPrefix(t *testing.T) {
	item := tmuxActionItem{
		Label:    "Open IDE",
		Action:   tmuxActionIDE,
		Keywords: "editor code",
	}
	if !actionMatchesQuery(item, "edi") {
		t.Fatalf("expected token prefix query to match")
	}
}

func TestActionMatchesQuery_DoesNotOvermatchShortQuery(t *testing.T) {
	item := tmuxActionItem{
		Label:    "Open shell (split down)",
		Action:   tmuxActionShellSplit,
		Keywords: "shell split pane ctrl+s s",
	}
	if actionMatchesQuery(item, "pr") {
		t.Fatalf("expected short query pr not to match shell action")
	}
}

func TestTmuxActionsModel_RebuildFiltered(t *testing.T) {
	m := newTmuxActionsModel("/tmp", true, false)
	m.query = "pull"
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

func TestTmuxActionsCommandWithAction_InjectsSourcePane(t *testing.T) {
	got := tmuxActionsCommandWithAction("/usr/local/bin/wtx", tmuxActionBack)
	if strings.Contains(got, "--source-pane") {
		t.Fatalf("did not expect source-pane flag in %q", got)
	}
	if want := "back_to_wtx"; !strings.Contains(got, want) {
		t.Fatalf("expected back action token %q in %q", want, got)
	}
}
