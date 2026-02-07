package main

import "testing"

func TestEnsureRequiredAtLeastApproved_UsesActualApprovalCount(t *testing.T) {
	required, known := ensureRequiredAtLeastApproved(2, true, 1, true)
	if required != 2 {
		t.Fatalf("expected required=2, got %d", required)
	}
	if !known {
		t.Fatalf("expected required to be known")
	}
}

func TestEnsureRequiredAtLeastApproved_LeavesUnknownUnchanged(t *testing.T) {
	required, known := ensureRequiredAtLeastApproved(0, false, 1, true)
	if required != 1 {
		t.Fatalf("expected required=1, got %d", required)
	}
	if !known {
		t.Fatalf("expected known=true")
	}
}
