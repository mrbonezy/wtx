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
		name        string
		state       string
		mergedAt    string
		isDraft     bool
		mergeable   string
		reviewOK    bool
		reviewReq   bool
		ci          PRCIState
		ciReq       bool
		unres       int
		known       bool
		commentsReq bool
		want        string
	}{
		{name: "merged wins", state: "OPEN", mergedAt: "2026-01-01T00:00:00Z", mergeable: "DIRTY", reviewOK: true, reviewReq: true, ci: PRCISuccess, ciReq: true, unres: 0, known: true, commentsReq: true, want: "merged"},
		{name: "closed wins", state: "CLOSED", reviewOK: true, reviewReq: true, ci: PRCISuccess, ciReq: true, unres: 0, known: true, commentsReq: true, want: "closed"},
		{name: "conflict before can-merge", state: "OPEN", mergeable: "DIRTY", reviewOK: true, reviewReq: true, ci: PRCISuccess, ciReq: true, unres: 0, known: true, commentsReq: true, want: "conflict"},
		{name: "can-merge", state: "OPEN", reviewOK: true, reviewReq: true, ci: PRCISuccess, ciReq: true, unres: 0, known: true, commentsReq: true, want: "can-merge"},
		{name: "awaiting-review", state: "OPEN", reviewOK: false, reviewReq: true, ci: PRCISuccess, ciReq: true, unres: 0, known: true, commentsReq: true, want: "awaiting-review"},
		{name: "awaiting-ci", state: "OPEN", reviewOK: true, reviewReq: true, ci: PRCIInProgress, ciReq: true, unres: 0, known: true, commentsReq: true, want: "awaiting-ci"},
		{name: "awaiting-comments", state: "OPEN", reviewOK: true, reviewReq: true, ci: PRCISuccess, ciReq: true, unres: 2, known: true, commentsReq: true, want: "awaiting-comments"},
		{name: "review not required", state: "OPEN", reviewOK: false, reviewReq: false, ci: PRCISuccess, ciReq: true, unres: 0, known: true, commentsReq: true, want: "can-merge"},
		{name: "ci not required", state: "OPEN", reviewOK: true, reviewReq: true, ci: PRCIInProgress, ciReq: false, unres: 0, known: true, commentsReq: true, want: "can-merge"},
		{name: "comments not required", state: "OPEN", reviewOK: true, reviewReq: true, ci: PRCISuccess, ciReq: true, unres: 2, known: true, commentsReq: false, want: "can-merge"},
		{name: "draft fallback", state: "OPEN", isDraft: true, reviewOK: true, reviewReq: true, ci: PRCISuccess, ciReq: true, unres: 2, known: false, commentsReq: true, want: "draft"},
		{name: "open fallback", state: "OPEN", reviewOK: true, reviewReq: true, ci: PRCISuccess, ciReq: true, unres: 2, known: false, commentsReq: true, want: "open"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := computePRStatus(tc.state, tc.mergedAt, tc.isDraft, tc.mergeable, tc.reviewOK, tc.reviewReq, tc.ci, tc.ciReq, tc.unres, tc.known, tc.commentsReq)
			if got != tc.want {
				t.Fatalf("expected %q, got %q", tc.want, got)
			}
		})
	}
}
