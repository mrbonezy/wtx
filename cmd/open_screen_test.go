package cmd

import (
	"strings"
	"testing"
)

func TestRenderOpenScreenTmuxHintShownBelowUpdateHint(t *testing.T) {
	t.Setenv("WTX_DISABLE_TMUX", "1")
	t.Setenv("TMUX", "")

	updateHint := "Update available: v1.2.3"
	view := renderOpenScreen(model{
		openStage:   openStageMain,
		updateHint:  updateHint,
		openLoading: true,
	})

	tmuxHint := "tmux not detected; status line is disabled."
	updateIdx := strings.Index(view, updateHint)
	tmuxIdx := strings.Index(view, tmuxHint)
	if updateIdx == -1 {
		t.Fatalf("expected update hint to be present, got %q", view)
	}
	if tmuxIdx == -1 {
		t.Fatalf("expected tmux hint to be present, got %q", view)
	}
	if tmuxIdx <= updateIdx {
		t.Fatalf("expected tmux hint below update hint, got %q", view)
	}
}

func TestRenderOpenScreenAlignsPRColumnByDetectedMaxBranchLength(t *testing.T) {
	t.Setenv("WTX_DISABLE_TMUX", "1")
	t.Setenv("TMUX", "")

	view := renderOpenScreen(model{
		openStage: openStageMain,
		openBranches: []openBranchOption{
			{Name: "short", HasPR: true, PRNumber: 1},
			{Name: "much-longer-branch-name", HasPR: true, PRNumber: 2},
		},
		openLockedBranches: []openBranchOption{
			{Name: "mid", HasPR: true, PRNumber: 3},
		},
	})

	shortLine := findRenderedLine(view, "short")
	longLine := findRenderedLine(view, "much-longer-branch-name")
	lockedLine := findRenderedLine(view, "mid")
	if shortLine == "" || longLine == "" || lockedLine == "" {
		t.Fatalf("expected rendered lines for all branches, got %q", view)
	}

	shortPR := strings.Index(shortLine, "#1")
	longPR := strings.Index(longLine, "#2")
	lockedPR := strings.Index(lockedLine, "#3")
	if shortPR == -1 || longPR == -1 || lockedPR == -1 {
		t.Fatalf("expected PR markers on all rows, got %q", view)
	}
	if shortPR != longPR || shortPR != lockedPR {
		t.Fatalf("expected aligned PR columns, got short=%d long=%d locked=%d\n%s", shortPR, longPR, lockedPR, view)
	}
}

func findRenderedLine(view string, needle string) string {
	for _, line := range strings.Split(view, "\n") {
		if strings.Contains(line, needle) {
			return line
		}
	}
	return ""
}

func TestOpenFilteredIndicesCapsSearchResults(t *testing.T) {
	branches := make([]openBranchOption, 0, 500)
	for i := 0; i < 500; i++ {
		branches = append(branches, openBranchOption{Name: "feature/load-test"})
	}
	got := openFilteredIndices("feature", branches)
	if len(got) != openSearchMatchLimit {
		t.Fatalf("expected %d results, got %d", openSearchMatchLimit, len(got))
	}
}

func TestBuildOpenBranchLists_NoPRLoadingInSearchMode(t *testing.T) {
	openBranches, lockedBranches, _ := buildOpenBranchLists([]string{"main", "feature/a"}, nil, false)
	for _, b := range openBranches {
		if b.PRLoading {
			t.Fatalf("expected open branch PRLoading=false for search mode")
		}
	}
	for _, b := range lockedBranches {
		if b.PRLoading {
			t.Fatalf("expected locked branch PRLoading=false for search mode")
		}
	}
}

func TestOpenVisibleFilteredIndices_KeepsSelectionVisible(t *testing.T) {
	filtered := make([]int, 0, 50)
	for i := 0; i < 50; i++ {
		filtered = append(filtered, i)
	}
	visible, trimmed := openVisibleFilteredIndices(filtered, 30, 10)
	if !trimmed {
		t.Fatalf("expected trimmed=true")
	}
	if len(visible) != 10 {
		t.Fatalf("expected 10 visible entries, got %d", len(visible))
	}
	found := false
	for _, idx := range visible {
		if idx == 29 {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected selected index to be visible")
	}
}

func TestOpenBranchRenderLimit_Clamped(t *testing.T) {
	if got := openBranchRenderLimit(0); got != 20 {
		t.Fatalf("expected default limit 20, got %d", got)
	}
	if got := openBranchRenderLimit(200); got != 40 {
		t.Fatalf("expected max-clamped limit 40, got %d", got)
	}
	if got := openBranchRenderLimit(12); got != 8 {
		t.Fatalf("expected min-clamped limit 8, got %d", got)
	}
}
