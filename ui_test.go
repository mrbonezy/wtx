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
