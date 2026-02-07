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

func TestComputePRStatus_Priority(t *testing.T) {
	tests := []struct {
		name      string
		state     string
		mergedAt  string
		isDraft   bool
		mergeable string
		reviewOK  bool
		ci        PRCIState
		unres     int
		known     bool
		want      string
	}{
		{name: "merged wins", state: "OPEN", mergedAt: "2026-01-01T00:00:00Z", mergeable: "DIRTY", reviewOK: true, ci: PRCISuccess, unres: 0, known: true, want: "merged"},
		{name: "closed wins", state: "CLOSED", reviewOK: true, ci: PRCISuccess, unres: 0, known: true, want: "closed"},
		{name: "conflict before can-merge", state: "OPEN", mergeable: "DIRTY", reviewOK: true, ci: PRCISuccess, unres: 0, known: true, want: "conflict"},
		{name: "can-merge", state: "OPEN", reviewOK: true, ci: PRCISuccess, unres: 0, known: true, want: "can-merge"},
		{name: "awaiting-review", state: "OPEN", reviewOK: false, ci: PRCISuccess, unres: 0, known: true, want: "awaiting-review"},
		{name: "awaiting-ci", state: "OPEN", reviewOK: true, ci: PRCIInProgress, unres: 0, known: true, want: "awaiting-ci"},
		{name: "awaiting-comments", state: "OPEN", reviewOK: true, ci: PRCISuccess, unres: 2, known: true, want: "awaiting-comments"},
		{name: "draft fallback", state: "OPEN", isDraft: true, reviewOK: true, ci: PRCISuccess, unres: 2, known: false, want: "draft"},
		{name: "open fallback", state: "OPEN", reviewOK: true, ci: PRCISuccess, unres: 2, known: false, want: "open"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := computePRStatus(tc.state, tc.mergedAt, tc.isDraft, tc.mergeable, tc.reviewOK, tc.ci, tc.unres, tc.known)
			if got != tc.want {
				t.Fatalf("expected %q, got %q", tc.want, got)
			}
		})
	}
}
