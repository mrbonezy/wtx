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
		key         string
		loadedKey   string
		fetchingKey string
		want        bool
	}{
		{name: "new key", key: "a", loadedKey: "", fetchingKey: "", want: true},
		{name: "loaded key", key: "a", loadedKey: "a", fetchingKey: "", want: false},
		{name: "fetching key", key: "a", loadedKey: "", fetchingKey: "a", want: false},
		{name: "empty key", key: "", loadedKey: "", fetchingKey: "", want: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := shouldFetchByBranch(tc.key, tc.loadedKey, tc.fetchingKey)
			if got != tc.want {
				t.Fatalf("expected %v, got %v", tc.want, got)
			}
		})
	}
}
