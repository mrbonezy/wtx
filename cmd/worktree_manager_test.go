package cmd

import (
	"errors"
	"strings"
	"testing"
)

func TestCommandErrorWithOutput_PrefersCommandOutput(t *testing.T) {
	fallback := errors.New("exit status 128")
	err := commandErrorWithOutput(fallback, []byte("fatal: worktree contains unstaged changes\n"))
	if err == nil {
		t.Fatalf("expected an error")
	}
	if !strings.Contains(err.Error(), "unstaged changes") {
		t.Fatalf("expected stderr message, got %q", err.Error())
	}
}

func TestCommandErrorWithOutput_FallsBackToOriginalError(t *testing.T) {
	fallback := errors.New("exit status 128")
	err := commandErrorWithOutput(fallback, []byte("   \n\t"))
	if err == nil {
		t.Fatalf("expected an error")
	}
	if err.Error() != fallback.Error() {
		t.Fatalf("expected fallback error %q, got %q", fallback.Error(), err.Error())
	}
}

func TestChooseFallbackBaseNoRemote_PrefersMainWhenPresent(t *testing.T) {
	got := chooseFallbackBaseNoRemote(true, "feature/test")
	if got != "main" {
		t.Fatalf("expected main, got %q", got)
	}
}

func TestChooseFallbackBaseNoRemote_UsesCurrentWhenMainMissing(t *testing.T) {
	got := chooseFallbackBaseNoRemote(false, "feature/test")
	if got != "feature/test" {
		t.Fatalf("expected current branch, got %q", got)
	}
}

func TestChooseFallbackBaseNoRemote_FallsBackToMainOnDetached(t *testing.T) {
	got := chooseFallbackBaseNoRemote(false, "detached")
	if got != "main" {
		t.Fatalf("expected main fallback, got %q", got)
	}
}

func TestFetchRemoteAndRefForBaseRef(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		baseRef    string
		remotes    []string
		preferred  string
		wantRemote string
		wantRef    string
		wantOK     bool
	}{
		{
			name:       "explicit remote branch",
			baseRef:    "origin/main",
			remotes:    []string{"origin"},
			preferred:  "origin",
			wantRemote: "origin",
			wantRef:    "main",
			wantOK:     true,
		},
		{
			name:       "explicit refs remotes branch",
			baseRef:    "refs/remotes/origin/release/v1",
			remotes:    []string{"origin"},
			preferred:  "origin",
			wantRemote: "origin",
			wantRef:    "release/v1",
			wantOK:     true,
		},
		{
			name:       "plain branch with preferred remote",
			baseRef:    "feature/new-ui",
			remotes:    []string{"origin"},
			preferred:  "origin",
			wantRemote: "origin",
			wantRef:    "feature/new-ui",
			wantOK:     true,
		},
		{
			name:       "refs heads with preferred remote",
			baseRef:    "refs/heads/main",
			remotes:    []string{"origin"},
			preferred:  "origin",
			wantRemote: "origin",
			wantRef:    "main",
			wantOK:     true,
		},
		{
			name:       "no remotes",
			baseRef:    "main",
			remotes:    nil,
			preferred:  "",
			wantRemote: "",
			wantRef:    "",
			wantOK:     false,
		},
		{
			name:       "head",
			baseRef:    "HEAD",
			remotes:    []string{"origin"},
			preferred:  "origin",
			wantRemote: "",
			wantRef:    "",
			wantOK:     false,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			gotRemote, gotRef, gotOK := fetchRemoteAndRefForBaseRef(tc.baseRef, tc.remotes, tc.preferred)
			if gotRemote != tc.wantRemote || gotRef != tc.wantRef || gotOK != tc.wantOK {
				t.Fatalf("fetchRemoteAndRefForBaseRef(%q) = (%q, %q, %t), want (%q, %q, %t)", tc.baseRef, gotRemote, gotRef, gotOK, tc.wantRemote, tc.wantRef, tc.wantOK)
			}
		})
	}
}

func TestIsExplicitRemoteBaseRef(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		baseRef string
		remotes []string
		want    bool
	}{
		{name: "remote short form", baseRef: "origin/main", remotes: []string{"origin"}, want: true},
		{name: "remote refs form", baseRef: "refs/remotes/origin/main", remotes: []string{"origin"}, want: true},
		{name: "other remote", baseRef: "upstream/main", remotes: []string{"origin", "upstream"}, want: true},
		{name: "local branch", baseRef: "main", remotes: []string{"origin"}, want: false},
		{name: "head", baseRef: "HEAD", remotes: []string{"origin"}, want: false},
	}
	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := isExplicitRemoteBaseRef(tc.baseRef, tc.remotes)
			if got != tc.want {
				t.Fatalf("isExplicitRemoteBaseRef(%q)=%v, want %v", tc.baseRef, got, tc.want)
			}
		})
	}
}
