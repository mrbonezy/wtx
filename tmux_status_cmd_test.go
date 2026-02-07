package main

import "testing"

func TestReviewLabel_UsesReviewProgressWhenRequiredKnown(t *testing.T) {
	pr := PRData{ReviewApproved: 2, ReviewRequired: 2, UnresolvedComments: 1}
	if got := reviewLabel(pr); got != "2/2 u:1" {
		t.Fatalf("expected 2/2 label, got %q", got)
	}
}

func TestReviewLabel_UsesKnownApprovalWhenNoRequiredCount(t *testing.T) {
	pr := PRData{ReviewApproved: 3, ReviewKnown: true, UnresolvedComments: 0}
	if got := reviewLabel(pr); got != "3/3 u:0" {
		t.Fatalf("expected 3/3 label, got %q", got)
	}
}

func TestReviewLabel_NormalizesRequiredWhenApprovalsAreHigher(t *testing.T) {
	pr := PRData{ReviewApproved: 2, ReviewRequired: 1, ReviewKnown: true, UnresolvedComments: 0}
	if got := reviewLabel(pr); got != "2/2 u:0" {
		t.Fatalf("expected 2/2 label, got %q", got)
	}
}
