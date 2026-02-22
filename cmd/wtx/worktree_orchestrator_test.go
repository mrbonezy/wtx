package main

import "testing"

func TestResolveOpenTargetSlot_ExistingBranchUsesAttachedWorktree(t *testing.T) {
	o := &WorktreeOrchestrator{}
	slots := []openSlotState{
		{Path: "/wt/1", Branch: "feat/cli", Locked: true},
		{Path: "/wt/2", Branch: "main"},
	}

	got, ok := o.ResolveOpenTargetSlot(slots, "feat/cli", false)
	if !ok {
		t.Fatalf("expected slot")
	}
	if got.Path != "/wt/1" {
		t.Fatalf("expected attached worktree /wt/1, got %q", got.Path)
	}
}

func TestResolveOpenTargetSlot_ExistingBranchFallsBackToAvailableSlot(t *testing.T) {
	o := &WorktreeOrchestrator{}
	slots := []openSlotState{
		{Path: "/wt/1", Branch: "main", Locked: true},
		{Path: "/wt/2", Branch: "dev"},
	}

	got, ok := o.ResolveOpenTargetSlot(slots, "feat/cli", false)
	if !ok {
		t.Fatalf("expected slot")
	}
	if got.Path != "/wt/2" {
		t.Fatalf("expected available fallback /wt/2, got %q", got.Path)
	}
}

func TestResolveOpenTargetSlot_NewBranchIgnoresLockedMatchingSlot(t *testing.T) {
	o := &WorktreeOrchestrator{}
	slots := []openSlotState{
		{Path: "/wt/1", Branch: "feat/new", Locked: true},
		{Path: "/wt/2", Branch: "main"},
	}

	got, ok := o.ResolveOpenTargetSlot(slots, "feat/new", true)
	if !ok {
		t.Fatalf("expected slot")
	}
	if got.Path != "/wt/2" {
		t.Fatalf("expected available slot /wt/2, got %q", got.Path)
	}
}

func TestResolveOpenTargetSlot_NoSlotAvailable(t *testing.T) {
	o := &WorktreeOrchestrator{}
	slots := []openSlotState{
		{Path: "/wt/1", Branch: "main", Locked: true},
		{Path: "/wt/2", Branch: "dev", Dirty: true},
	}

	_, ok := o.ResolveOpenTargetSlot(slots, "feat/cli", false)
	if ok {
		t.Fatalf("expected no slot")
	}
}
