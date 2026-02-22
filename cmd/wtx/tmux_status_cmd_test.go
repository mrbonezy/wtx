package main

import "testing"

func TestGHAPIStatusLabel_Mapping(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "conflict", in: "conflict", want: "conflict"},
		{name: "awaiting ci", in: "awaiting-ci", want: "waiting for checks"},
		{name: "awaiting review", in: "awaiting-review", want: "awaiting approval"},
		{name: "can merge", in: "can-merge", want: "mergeable"},
		{name: "awaiting comments", in: "awaiting-comments", want: "awaiting comments"},
		{name: "draft", in: "draft", want: "draft"},
		{name: "open", in: "open", want: "open"},
		{name: "closed", in: "closed", want: "closed"},
		{name: "merged", in: "merged", want: "merged"},
		{name: "unknown", in: "something-else", want: "-"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := ghAPIStatusLabel(PRData{Status: tc.in})
			if got != tc.want {
				t.Fatalf("expected %q, got %q", tc.want, got)
			}
		})
	}
}

func TestPRLabel_ShowsNumberOnly(t *testing.T) {
	if got := prLabel(PRData{Number: 12, Status: "open"}); got != "#12" {
		t.Fatalf("expected #12, got %q", got)
	}
}

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
