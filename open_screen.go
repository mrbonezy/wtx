package main

import (
	"fmt"
	"os/exec"
	"sort"
	"strings"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/muesli/termenv"
)

type openBranchOption struct {
	Name      string
	PRNumber  int
	PRURL     string
	HasPR     bool
	PRLoading bool
}

type openSlotState struct {
	Path      string
	Branch    string
	Locked    bool
	Dirty     bool
	HasPR     bool
	PRNumber  int
	PRLoading bool
}

type openScreenLoadedMsg struct {
	status         WorktreeStatus
	branches       []openBranchOption
	lockedBranches []openBranchOption
	slots          []openSlotState
	prBranches     []string
	fetchID        string
	err            error
}

type openScreenPRDataMsg struct {
	byBranch map[string]PRData
	fetchID  string
	err      error
}

type openScreenDirtyMsg struct {
	dirtyByPath map[string]bool
}

func loadOpenScreenCmd(orchestrator *WorktreeOrchestrator, mgr *WorktreeManager) tea.Cmd {
	return func() tea.Msg {
		if orchestrator == nil || mgr == nil {
			return openScreenLoadedMsg{err: fmt.Errorf("open screen unavailable")}
		}
		status := orchestrator.Status()
		if status.Err != nil {
			return openScreenLoadedMsg{status: status, err: status.Err}
		}
		branches, err := mgr.ListLocalBranchesByRecentUse()
		if err != nil {
			return openScreenLoadedMsg{status: status, err: err}
		}

		slots := make([]openSlotState, len(status.Worktrees))
		lockedBranches := make(map[string]bool, len(status.Worktrees))
		seenPR := make(map[string]bool, len(branches)+len(status.Worktrees))
		prBranches := make([]string, 0, len(branches)+len(status.Worktrees))

		for i, wt := range status.Worktrees {
			slots[i] = openSlotState{
				Path:      wt.Path,
				Branch:    wt.Branch,
				Locked:    !wt.Available,
				PRLoading: true,
			}
			if locked, err := worktreeLockedByAny(orchestrator, status.RepoRoot, wt.Path); err == nil && locked {
				slots[i].Locked = true
			}
			if slots[i].Locked {
				lockedBranches[strings.TrimSpace(slots[i].Branch)] = true
			}
			name := strings.TrimSpace(slots[i].Branch)
			if name != "" && name != "detached" && !seenPR[name] {
				seenPR[name] = true
				prBranches = append(prBranches, name)
			}
		}

		openBranches := make([]openBranchOption, 0, len(branches))
		lockedList := make([]openBranchOption, 0, len(lockedBranches))
		lockedSeen := make(map[string]bool, len(lockedBranches))
		for _, branch := range branches {
			name := strings.TrimSpace(branch)
			if name == "" {
				continue
			}
			if lockedBranches[name] {
				lockedList = append(lockedList, openBranchOption{
					Name:      name,
					PRLoading: true,
				})
				lockedSeen[name] = true
				if !seenPR[name] {
					seenPR[name] = true
					prBranches = append(prBranches, name)
				}
				continue
			}
			openBranches = append(openBranches, openBranchOption{
				Name:      name,
				PRLoading: true,
			})
			if !seenPR[name] {
				seenPR[name] = true
				prBranches = append(prBranches, name)
			}
		}
		missingLocked := make([]string, 0, len(lockedBranches))
		for name := range lockedBranches {
			if !lockedSeen[name] {
				missingLocked = append(missingLocked, name)
			}
		}
		sort.Strings(missingLocked)
		for _, name := range missingLocked {
			lockedList = append(lockedList, openBranchOption{
				Name:      name,
				PRLoading: true,
			})
			if !seenPR[name] {
				seenPR[name] = true
				prBranches = append(prBranches, name)
			}
		}

		return openScreenLoadedMsg{
			status:         status,
			branches:       openBranches,
			lockedBranches: lockedList,
			slots:          slots,
			prBranches:     prBranches,
			fetchID:        fmt.Sprintf("%d", time.Now().UnixNano()),
		}
	}
}

func fetchDirtyStatusCmd(paths []string) tea.Cmd {
	return func() tea.Msg {
		result := make(map[string]bool, len(paths))
		var mu sync.Mutex
		var wg sync.WaitGroup
		for _, p := range paths {
			wg.Add(1)
			go func(path string) {
				defer wg.Done()
				dirty, err := worktreeDirty(path)
				if err == nil {
					mu.Lock()
					result[path] = dirty
					mu.Unlock()
				}
			}(p)
		}
		wg.Wait()
		return openScreenDirtyMsg{dirtyByPath: result}
	}
}

func fetchOpenPRDataCmd(orchestrator *WorktreeOrchestrator, repoRoot string, branches []string, fetchID string) tea.Cmd {
	return func() tea.Msg {
		if orchestrator == nil {
			return openScreenPRDataMsg{byBranch: map[string]PRData{}, fetchID: fetchID}
		}
		byBranch, err := orchestrator.PRDataForBranchesWithError(repoRoot, branches, false)
		if byBranch == nil {
			byBranch = map[string]PRData{}
		}
		return openScreenPRDataMsg{byBranch: byBranch, fetchID: fetchID, err: err}
	}
}

func applyPRDataToOpenState(branches *[]openBranchOption, lockedBranches *[]openBranchOption, slots *[]openSlotState, byBranch map[string]PRData) {
	if branches != nil {
		for i := range *branches {
			b := strings.TrimSpace((*branches)[i].Name)
			(*branches)[i].PRLoading = false
			(*branches)[i].HasPR = false
			(*branches)[i].PRNumber = 0
			(*branches)[i].PRURL = ""
			if pr, ok := byBranch[b]; ok && pr.Number > 0 {
				(*branches)[i].HasPR = true
				(*branches)[i].PRNumber = pr.Number
				(*branches)[i].PRURL = pr.URL
			}
		}
	}
	if lockedBranches != nil {
		for i := range *lockedBranches {
			b := strings.TrimSpace((*lockedBranches)[i].Name)
			(*lockedBranches)[i].PRLoading = false
			(*lockedBranches)[i].HasPR = false
			(*lockedBranches)[i].PRNumber = 0
			(*lockedBranches)[i].PRURL = ""
			if pr, ok := byBranch[b]; ok && pr.Number > 0 {
				(*lockedBranches)[i].HasPR = true
				(*lockedBranches)[i].PRNumber = pr.Number
				(*lockedBranches)[i].PRURL = pr.URL
			}
		}
	}
	if slots != nil {
		for i := range *slots {
			b := strings.TrimSpace((*slots)[i].Branch)
			(*slots)[i].PRLoading = false
			(*slots)[i].HasPR = false
			(*slots)[i].PRNumber = 0
			if pr, ok := byBranch[b]; ok && pr.Number > 0 {
				(*slots)[i].HasPR = true
				(*slots)[i].PRNumber = pr.Number
			}
		}
	}
}

func clampOpenSelection(index int, branchCount int) int {
	maxIndex := branchCount
	if index < 0 {
		return 0
	}
	if index > maxIndex {
		return maxIndex
	}
	return index
}

func renderOpenScreen(m model) string {
	var b strings.Builder
	if m.openCreating {
		elapsed := ""
		if !m.openCreatingStartedAt.IsZero() {
			elapsed = fmt.Sprintf(" (%ds)", int(time.Since(m.openCreatingStartedAt).Seconds()))
		}
		branch := strings.TrimSpace(m.openTargetBranch)
		if branch == "" {
			branch = "branch"
		}
		b.WriteString(m.spinner.View())
		b.WriteString(" ")
		if m.openTargetIsNew && strings.TrimSpace(m.openTargetBaseRef) != "" {
			b.WriteString(fmt.Sprintf("Creating %s from %s%s...\n", branch, m.openTargetBaseRef, elapsed))
		} else {
			b.WriteString(fmt.Sprintf("Switching to %s%s...\n", branch, elapsed))
		}
		return b.String()
	}
	if m.openShowDebug {
		b.WriteString("Worktrees debug:\n")
		b.WriteString(secondaryStyle.Render(fmt.Sprintf("  %-12s %-24s %s", "State", "Branch", "Path")) + "\n")
		for i, slot := range m.openSlots {
			cursor := "  "
			rowRenderer := secondaryStyle.Render
			if i == m.openDebugIndex {
				cursor = "> "
				rowRenderer = selectorSelectedStyle.Render
			}
			state := debugWorktreeState(slot)
			line := fmt.Sprintf("%s%-12s %-24s %s", cursor, state, slot.Branch, slot.Path)
			b.WriteString(rowRenderer(line) + "\n")
		}
		if len(m.openSlots) == 0 {
			b.WriteString("  (no worktrees)\n")
		}
		if m.openDebugCreating {
			b.WriteString("\n")
			b.WriteString("New worktree branch:\n")
			b.WriteString("  " + inputStyle.Render(m.newBranchInput.View()) + "\n")
		}
		if m.openLoadErr != "" {
			b.WriteString("\n")
			b.WriteString(errorStyle.Render("Error: " + m.openLoadErr))
			b.WriteString("\n")
		}
		if m.errMsg != "" {
			b.WriteString("\n")
			b.WriteString(errorStyle.Render(m.errMsg))
			b.WriteString("\n")
		}
		if m.updateHint != "" {
			b.WriteString("\n")
			b.WriteString(warnStyle.Render(m.updateHint))
			b.WriteString("\n")
		}
		b.WriteString("\nUse up/down to select. d delete selected (with confirm). u unlock selected (with confirm). n new worktree.\n")
		if m.openDebugCreating {
			b.WriteString("Type branch name, enter to create, esc to cancel. ")
		}
		b.WriteString("Ctrl+R refreshes. Esc/Ctrl+D back. q quits.\n")
		return b.String()
	}
	if m.openStage == openStageNewBranchConfig {
		if m.openNewBranchForm != nil {
			b.WriteString(m.openNewBranchForm.View())
			b.WriteString("\n")
		}
		if m.openLoadErr != "" {
			b.WriteString("\n")
			b.WriteString(errorStyle.Render("Error: " + m.openLoadErr))
			b.WriteString("\n")
		}
		if m.errMsg != "" {
			b.WriteString("\n")
			b.WriteString(errorStyle.Render(m.errMsg))
			b.WriteString("\n")
		}
		if m.updateHint != "" {
			b.WriteString("\n")
			b.WriteString(warnStyle.Render(m.updateHint))
			b.WriteString("\n")
		}
		return b.String()
	}
	if m.openStage == openStagePickWorktree {
		b.WriteString("No clean available worktree. Choose target:\n")
		createLine := "  + Create new worktree"
		if m.openPickIndex == 0 {
			createLine = "> + Create new worktree"
			b.WriteString(selectorSelectedStyle.Render(createLine) + "\n")
		} else {
			b.WriteString(actionNormalStyle.Render(createLine) + "\n")
		}
		for i, slot := range m.openSlots {
			rowIndex := i + 1
			cursor := "  "
			render := actionNormalStyle.Render
			if rowIndex == m.openPickIndex {
				cursor = "> "
				render = selectorSelectedStyle.Render
			}
			state := debugWorktreeState(slot)
			line := fmt.Sprintf("%s%-12s %-24s %s", cursor, state, slot.Branch, slot.Path)
			b.WriteString(render(line) + "\n")
		}
		if m.openLoadErr != "" {
			b.WriteString("\n")
			b.WriteString(errorStyle.Render("Error: " + m.openLoadErr))
			b.WriteString("\n")
		}
		if m.errMsg != "" {
			b.WriteString("\n")
			b.WriteString(errorStyle.Render(m.errMsg))
			b.WriteString("\n")
		}
		if m.warnMsg != "" {
			b.WriteString("\n")
			b.WriteString(warnStyle.Render(m.warnMsg))
			b.WriteString("\n")
		}
		if m.updateHint != "" {
			b.WriteString("\n")
			b.WriteString(warnStyle.Render(m.updateHint))
			b.WriteString("\n")
		}
		b.WriteString("\nUse up/down to choose, enter to select. Esc goes back. Ctrl+R refreshes (auto-refresh every 2s).\n")
		return b.String()
	}
	b.WriteString("Choose branch:\n")
	newBranchLine := "  <new branch>"
	if m.openSelected == 0 {
		newBranchLine = "> <new branch>"
		b.WriteString(actionSelectedStyle.Render(newBranchLine) + "\n")
	} else {
		b.WriteString(actionNormalStyle.Render(newBranchLine) + "\n")
	}
	filtered := openFilteredIndices(m.openTypeahead, m.openBranches)
	for _, branchIndex := range filtered {
		branch := m.openBranches[branchIndex]
		cursor := "  "
		if m.openSelected == branchIndex+1 {
			cursor = "> "
		}
		pr := "-"
		if branch.PRLoading && m.openLoading {
			pr = m.ghSpinner.View()
		} else if branch.HasPR && branch.PRNumber > 0 {
			pr = fmt.Sprintf("#%d", branch.PRNumber)
			if strings.TrimSpace(branch.PRURL) != "" {
				pr = termenv.Hyperlink(branch.PRURL, pr)
			}
		}
		line := fmt.Sprintf("%s%-40s %s", cursor, branch.Name, pr)
		if m.openSelected == branchIndex+1 {
			b.WriteString(actionSelectedStyle.Render(line) + "\n")
		} else {
			b.WriteString(actionNormalStyle.Render(line) + "\n")
		}
	}
	if len(filtered) == 0 && len(m.openBranches) == 0 {
		b.WriteString("  No local branches available.\n")
	} else if len(filtered) == 0 && strings.TrimSpace(m.openTypeahead) != "" {
		b.WriteString("  No matching branches.\n")
	}
	if strings.TrimSpace(m.openTypeahead) != "" {
		b.WriteString("\n")
		b.WriteString(secondaryStyle.Render("Search: " + m.openTypeahead))
		b.WriteString("\n")
	}
	if len(m.openLockedBranches) > 0 {
		b.WriteString("\n")
		b.WriteString(secondaryStyle.Render(fmt.Sprintf("Locked branches (%d):", len(m.openLockedBranches))) + "\n")
		for _, branch := range m.openLockedBranches {
			pr := "-"
			if branch.PRLoading && m.openLoading {
				pr = m.ghSpinner.View()
			} else if branch.HasPR && branch.PRNumber > 0 {
				pr = fmt.Sprintf("#%d", branch.PRNumber)
				if strings.TrimSpace(branch.PRURL) != "" {
					pr = termenv.Hyperlink(branch.PRURL, pr)
				}
			}
			line := fmt.Sprintf("  %-40s %s", branch.Name, pr)
			b.WriteString(secondaryStyle.Render(line) + "\n")
		}
	}
	if m.openLoadErr != "" {
		b.WriteString("\n")
		b.WriteString(errorStyle.Render("Error: " + m.openLoadErr))
		b.WriteString("\n")
	}
	if m.errMsg != "" {
		b.WriteString("\n")
		b.WriteString(errorStyle.Render(m.errMsg))
		b.WriteString("\n")
	}
	if m.updateHint != "" {
		b.WriteString("\n")
		b.WriteString(warnStyle.Render(m.updateHint))
		b.WriteString("\n")
	}

	b.WriteString("\n")
	b.WriteString("Use up/down or type to search by branch/PR. Enter selects. Ctrl+R refreshes. Ctrl+D debug. q quits.\n")
	return b.String()
}

func debugWorktreeState(slot openSlotState) string {
	if slot.Locked {
		return "in use"
	}
	if slot.Dirty {
		return "unclean"
	}
	return "clean"
}

func findReusableOpenSlot(slots []openSlotState, branch string) (openSlotState, bool) {
	want := strings.TrimSpace(branch)
	for _, slot := range slots {
		if strings.TrimSpace(slot.Branch) != want {
			continue
		}
		if slot.Locked || slot.Dirty {
			continue
		}
		return slot, true
	}
	return openSlotState{}, false
}

func findAnyAvailableOpenSlot(slots []openSlotState) (openSlotState, bool) {
	for _, slot := range slots {
		if slot.Locked || slot.Dirty {
			continue
		}
		return slot, true
	}
	return openSlotState{}, false
}

func openPickRowCount(slots []openSlotState) int {
	return len(slots) + 1
}

func clampOpenPickIndex(index int, slots []openSlotState) int {
	maxIndex := openPickRowCount(slots) - 1
	if maxIndex < 0 {
		return 0
	}
	if index < 0 {
		return 0
	}
	if index > maxIndex {
		return maxIndex
	}
	return index
}

func openTypeaheadMatchIndex(query string, branches []openBranchOption) (int, bool) {
	filtered := openFilteredIndices(query, branches)
	if len(filtered) == 0 {
		return 0, false
	}
	return filtered[0] + 1, true
}

func openFilteredIndices(query string, branches []openBranchOption) []int {
	q := strings.ToLower(strings.TrimSpace(query))
	if q == "" {
		out := make([]int, 0, len(branches))
		for i := range branches {
			out = append(out, i)
		}
		return out
	}
	out := make([]int, 0, len(branches))
	qNum := strings.TrimPrefix(q, "#")
	for i, branch := range branches {
		name := strings.ToLower(strings.TrimSpace(branch.Name))
		nameMatch := strings.Contains(name, q)
		prMatch := false
		if branch.HasPR && branch.PRNumber > 0 {
			num := fmt.Sprintf("%d", branch.PRNumber)
			prMatch = strings.HasPrefix(num, qNum) || strings.Contains("#"+num, q)
		}
		if nameMatch || prMatch {
			out = append(out, i)
		}
	}
	return out
}

func moveOpenSelection(current int, delta int, filtered []int) int {
	options := make([]int, 0, len(filtered)+1)
	options = append(options, 0)
	for _, idx := range filtered {
		options = append(options, idx+1)
	}
	pos := 0
	for i, opt := range options {
		if opt == current {
			pos = i
			break
		}
	}
	pos += delta
	if pos < 0 {
		pos = 0
	}
	if pos >= len(options) {
		pos = len(options) - 1
	}
	return options[pos]
}

func ensureOpenSelectionVisible(current int, filtered []int) int {
	if current == 0 {
		return 0
	}
	for _, idx := range filtered {
		if current == idx+1 {
			return current
		}
	}
	if len(filtered) == 0 {
		return 0
	}
	return filtered[0] + 1
}

func worktreeDirty(path string) (bool, error) {
	gitBin, err := requireGitPath()
	if err != nil {
		return false, err
	}
	cmd := exec.Command(gitBin, "status", "--porcelain")
	cmd.Dir = path
	out, err := cmd.CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if msg == "" {
			return false, err
		}
		return false, fmt.Errorf("git status failed for %s: %s", path, msg)
	}
	return strings.TrimSpace(string(out)) != "", nil
}

func worktreeLockedByAny(orchestrator *WorktreeOrchestrator, repoRoot string, worktreePath string) (bool, error) {
	if orchestrator == nil || orchestrator.lockMgr == nil {
		return false, nil
	}
	available, err := orchestrator.lockMgr.IsAvailable(repoRoot, worktreePath)
	if err != nil {
		return false, err
	}
	return !available, nil
}
