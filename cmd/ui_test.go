package cmd

import (
	"errors"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
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

func TestNormalizeFetchForBaseRef(t *testing.T) {
	if got := normalizeFetchForBaseRef("main", true); got {
		t.Fatalf("expected local base ref to disable fetch")
	}
	if got := normalizeFetchForBaseRef("origin/main", true); !got {
		t.Fatalf("expected remote base ref to keep fetch enabled")
	}
	if got := normalizeFetchForBaseRef("main", false); got {
		t.Fatalf("expected local base ref to keep fetch disabled")
	}
}

func TestShouldPromptFetchDefault(t *testing.T) {
	if shouldPromptFetchDefault("main", false, true) {
		t.Fatalf("expected local base ref to suppress fetch-default prompt")
	}
	if !shouldPromptFetchDefault("origin/main", false, true) {
		t.Fatalf("expected remote base ref to prompt when fetch preference differs")
	}
	if shouldPromptFetchDefault("origin/main", true, true) {
		t.Fatalf("expected no prompt when fetch preference matches default")
	}
}

func TestLooksLikeLocalBranchRef(t *testing.T) {
	if !looksLikeLocalBranchRef("main") {
		t.Fatalf("expected main to be treated as local")
	}
	if looksLikeLocalBranchRef("origin/main") {
		t.Fatalf("expected origin/main to be treated as remote")
	}
}

func TestDraftBranchName(t *testing.T) {
	got := draftBranchName(time.Unix(1700000000, 0))
	if got != "draft-1700000000" {
		t.Fatalf("expected deterministic draft name, got %q", got)
	}
}

func TestModeBranchPick_AllowsTypingKAndJInFilter(t *testing.T) {
	m := newModel()
	m.mode = modeBranchPick
	m.branchOptions = []string{"main", "release/kilo", "feature/jump"}
	m.branchSuggestions = filterBranches(m.branchOptions, "")
	m.branchInput.Focus()

	updatedModel, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'k'}})
	updated := updatedModel.(model)
	if updated.branchInput.Value() != "k" {
		t.Fatalf("expected filter input to include k, got %q", updated.branchInput.Value())
	}

	updatedModel, _ = updated.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	updated = updatedModel.(model)
	if updated.branchInput.Value() != "kj" {
		t.Fatalf("expected filter input to include j, got %q", updated.branchInput.Value())
	}
}

func TestOpenScreenKeepsPreviousLoadErrorUntilPRDataResolves(t *testing.T) {
	m := newModel()
	m.openLoadErr = "previous fetch failed"

	updatedModel, _ := m.Update(openScreenLoadedMsg{
		status: WorktreeStatus{},
		branches: []openBranchOption{
			{Name: "feature/test", PRLoading: true},
		},
		prBranches: []string{"feature/test"},
		fetchID:    "fetch-1",
	})
	updated := updatedModel.(model)
	if updated.openLoadErr != "previous fetch failed" {
		t.Fatalf("expected prior open load error to remain while PR data fetch is pending, got %q", updated.openLoadErr)
	}
	if !updated.openLoading {
		t.Fatalf("expected open screen to remain loading while PR data fetch is pending")
	}

	updatedModel, _ = updated.Update(pollStatusTickMsg(time.Now()))
	updated = updatedModel.(model)
	if updated.openLoadErr != "previous fetch failed" {
		t.Fatalf("expected prior open load error to remain visible across poll tick, got %q", updated.openLoadErr)
	}

	updatedModel, _ = updated.Update(openScreenPRDataMsg{fetchID: "fetch-1", err: errors.New("gh lookup failed")})
	updated = updatedModel.(model)
	if updated.openLoadErr != "gh lookup failed" {
		t.Fatalf("expected PR fetch error to replace load error, got %q", updated.openLoadErr)
	}

	updatedModel, _ = updated.Update(openScreenLoadedMsg{
		status: WorktreeStatus{},
		branches: []openBranchOption{
			{Name: "feature/test", PRLoading: true},
		},
		prBranches: []string{"feature/test"},
		fetchID:    "fetch-2",
	})
	updated = updatedModel.(model)
	if updated.openLoadErr != "gh lookup failed" {
		t.Fatalf("expected latest load error to remain while next PR fetch is pending, got %q", updated.openLoadErr)
	}

	updatedModel, _ = updated.Update(openScreenPRDataMsg{fetchID: "fetch-2", byBranch: map[string]PRData{}})
	updated = updatedModel.(model)
	if updated.openLoadErr != "" {
		t.Fatalf("expected load error to clear after successful PR fetch, got %q", updated.openLoadErr)
	}
}

func TestOpenPickAllowsDirtyWorktreeWhenBranchMatchesTarget(t *testing.T) {
	m := newModel()
	m.mode = modeOpen
	m.openStage = openStagePickWorktree
	m.openTargetBranch = "feature/existing"
	m.openPickIndex = 1
	m.openSlots = []openSlotState{
		{Path: t.TempDir(), Branch: "feature/existing", Dirty: true},
	}

	updatedModel, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	updated := updatedModel.(model)
	if !updated.openCreating {
		t.Fatalf("expected selecting dirty matching branch slot to continue")
	}
	if updated.warnMsg != "" {
		t.Fatalf("expected no warning for dirty matching branch slot, got %q", updated.warnMsg)
	}
	if cmd == nil {
		t.Fatalf("expected command to be scheduled")
	}
}

func TestOpenScreenSearchLoadsAllBranchesOnFirstType(t *testing.T) {
	m := newModel()
	m.mode = modeOpen
	m.openStage = openStageMain
	m.openRecentBranches = []openBranchOption{{Name: "recent/one"}}
	m.openRecentLocked = []openBranchOption{}
	m.openBranches = append([]openBranchOption{}, m.openRecentBranches...)

	updatedModel, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'f'}})
	updated := updatedModel.(model)
	if updated.openTypeahead != "f" {
		t.Fatalf("expected typeahead to be updated, got %q", updated.openTypeahead)
	}
	if cmd == nil {
		t.Fatalf("expected loading-all-branches command on first search input")
	}

	updatedModel, _ = updated.Update(openAllBranchesLoadedMsg{
		branches:       []openBranchOption{{Name: "feature/a"}, {Name: "feature/b"}},
		lockedBranches: []openBranchOption{{Name: "locked/x"}},
	})
	updated = updatedModel.(model)
	if !updated.openSearchAllActive {
		t.Fatalf("expected search-all mode to activate after all branches load")
	}
	if len(updated.openBranches) != 2 {
		t.Fatalf("expected all-branch list to be active, got %d entries", len(updated.openBranches))
	}
}

func TestOpenScreenPRDataIgnoredForSearchAllBranchList(t *testing.T) {
	m := newModel()
	m.mode = modeOpen
	m.openSearchAllActive = true
	m.openFetchID = "fetch-1"
	m.openBranches = []openBranchOption{{Name: "feature/a", PRLoading: false}}
	m.openSlots = []openSlotState{{Branch: "feature/a"}}

	updatedModel, _ := m.Update(openScreenPRDataMsg{
		fetchID: "fetch-1",
		byBranch: map[string]PRData{
			"feature/a": {Number: 42, URL: "https://example.test/pr/42"},
		},
	})
	updated := updatedModel.(model)
	if updated.openBranches[0].HasPR {
		t.Fatalf("expected search-all branch rows to remain without PR data")
	}
}
