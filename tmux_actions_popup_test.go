package main

import "testing"

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
