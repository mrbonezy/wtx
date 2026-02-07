package main

import (
	"strings"
	"testing"
)

func TestRenderCreateProgress_NewBranchFromBase(t *testing.T) {
	m := model{
		creatingBranch:  "feature/test",
		creatingBaseRef: "origin/main",
	}
	got := renderCreateProgress(m)
	if !strings.Contains(got, "Provisioning") || !strings.Contains(got, "from") {
		t.Fatalf("expected provisioning-from message, got %q", got)
	}
	if !strings.Contains(got, "origin/main") {
		t.Fatalf("expected base ref in message, got %q", got)
	}
}

func TestRenderCreateProgress_ExistingBranch(t *testing.T) {
	m := model{
		creatingBranch:   "feature/test",
		creatingExisting: true,
	}
	got := renderCreateProgress(m)
	if !strings.Contains(got, "worktree for") {
		t.Fatalf("expected existing-branch provisioning message, got %q", got)
	}
}

func TestShouldFetchByBranch(t *testing.T) {
	tests := []struct {
		name        string
		page        int
		key         string
		loadedKey   string
		fetchingKey string
		want        bool
	}{
		{name: "worktree new key", page: worktreePage, key: "a", loadedKey: "", fetchingKey: "", want: true},
		{name: "worktree loaded key", page: worktreePage, key: "a", loadedKey: "a", fetchingKey: "", want: false},
		{name: "worktree fetching key", page: worktreePage, key: "a", loadedKey: "", fetchingKey: "a", want: false},
		{name: "prs page", page: prsPage, key: "a", loadedKey: "", fetchingKey: "", want: false},
		{name: "empty key", page: worktreePage, key: "", loadedKey: "", fetchingKey: "", want: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := shouldFetchByBranch(tc.page, tc.key, tc.loadedKey, tc.fetchingKey)
			if got != tc.want {
				t.Fatalf("expected %v, got %v", tc.want, got)
			}
		})
	}
}
