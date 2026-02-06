package main

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/paginator"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type model struct {
	mgr               *WorktreeManager
	orchestrator      *WorktreeOrchestrator
	runner            *Runner
	status            WorktreeStatus
	listIndex         int
	prIndex           int
	prs               []PRListData
	viewPager         paginator.Model
	ready             bool
	width             int
	height            int
	mode              uiMode
	branchInput       textinput.Model
	newBranchInput    textinput.Model
	spinner           spinner.Model
	ghSpinner         spinner.Model
	ghPendingByBranch map[string]bool
	ghDataByBranch    map[string]PRData
	ghLoadedKey       string
	ghFetchingKey     string
	forceGHRefresh    bool
	ghWarnMsg         string
	errMsg            string
	warnMsg           string
	creatingBranch    string
	deletePath        string
	deleteBranch      string
	unlockPath        string
	unlockBranch      string
	actionBranch      string
	actionIndex       int
	actionCreate      bool
	baseRefRefreshing bool
	branchOptions     []string
	branchSuggestions []string
	branchIndex       int
	pendingPath       string
	pendingBranch     string
	pendingOpenShell  bool
	pendingLock       *WorktreeLock
	autoActionPath    string
}

func (m model) PendingWorktree() (string, string, bool, *WorktreeLock) {
	return m.pendingPath, m.pendingBranch, m.pendingOpenShell, m.pendingLock
}

func newModel() model {
	lockMgr := NewLockManager()
	mgr := NewWorktreeManager("", lockMgr)
	orchestrator := NewWorktreeOrchestrator(mgr, lockMgr, NewGHManager())
	m := model{mgr: mgr, orchestrator: orchestrator, runner: NewRunner(lockMgr)}
	m.branchInput = newBranchInput()
	m.newBranchInput = newCreateBranchInput()
	m.spinner = newSpinner()
	m.ghSpinner = newGHSpinner()
	m.ghPendingByBranch = map[string]bool{}
	m.ghDataByBranch = map[string]PRData{}
	m.viewPager = newViewPager()
	return m
}

func (m model) Init() tea.Cmd {
	return tea.Batch(fetchStatusCmd(m.orchestrator), pollStatusTickCmd())
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case statusMsg:
		m.status = WorktreeStatus(msg)
		m.listIndex = clampListIndex(m.listIndex, m.status)
		m.prIndex = clampPRIndex(m.prIndex, m.prs)
		if m.autoActionPath != "" {
			if idx, wt, ok := findWorktreeByPath(m.status, m.autoActionPath); ok {
				m.listIndex = idx
				m.mode = modeAction
				m.actionCreate = false
				m.actionBranch = wt.Branch
				m.actionIndex = 0
				m.autoActionPath = ""
			}
		}
		m.ready = true
		key := ghDataKeyForStatus(m.status)
		if key == "" {
			m.ghPendingByBranch = map[string]bool{}
			m.ghDataByBranch = map[string]PRData{}
			m.prs = nil
			m.prIndex = 0
			m.ghLoadedKey = ""
			m.ghFetchingKey = ""
			m.ghWarnMsg = ""
			return m, nil
		}
		if key == m.ghLoadedKey || key == m.ghFetchingKey {
			applyPRDataToStatus(&m.status, m.ghDataByBranch)
		}
		if key == m.ghLoadedKey || key == m.ghFetchingKey {
			// Local status polling runs every second; avoid re-fetching GH unless branch snapshot changes.
			return m, nil
		}
		m.ghFetchingKey = key
		m.ghPendingByBranch = pendingBranchesByName(m.status)
		force := m.forceGHRefresh
		m.forceGHRefresh = false
		return m, tea.Batch(fetchGHDataCmd(m.orchestrator, m.status, key, force), m.ghSpinner.Tick)
	case ghDataMsg:
		if strings.TrimSpace(msg.repoRoot) == "" || strings.TrimSpace(m.status.RepoRoot) == "" {
			return m, nil
		}
		if msg.repoRoot != m.status.RepoRoot {
			return m, nil
		}
		if strings.TrimSpace(msg.key) == "" || msg.key != m.ghFetchingKey {
			// Ignore stale GH responses that raced with a newer status snapshot.
			return m, nil
		}
		m.ghWarnMsg = ghWarningFromErr(msg.err)
		m.ghDataByBranch = msg.byBranch
		m.prs = msg.prs
		m.prIndex = clampPRIndex(m.prIndex, m.prs)
		applyPRDataToStatus(&m.status, m.ghDataByBranch)
		m.ghPendingByBranch = map[string]bool{}
		m.ghLoadedKey = msg.key
		m.ghFetchingKey = ""
		m.listIndex = clampListIndex(m.listIndex, m.status)
		return m, nil
	case pollStatusTickMsg:
		if m.mode == modeList {
			return m, tea.Batch(fetchStatusCmd(m.orchestrator), pollStatusTickCmd())
		}
		return m, pollStatusTickCmd()
	case createWorktreeDoneMsg:
		m.mode = modeList
		m.creatingBranch = ""
		m.actionCreate = false
		if msg.err != nil {
			m.errMsg = msg.err.Error()
			return m, nil
		}
		m.errMsg = ""
		m.autoActionPath = strings.TrimSpace(msg.created.Path)
		return m, fetchStatusCmd(m.orchestrator)
	case baseRefResolvedMsg:
		m.baseRefRefreshing = false
		ref := strings.TrimSpace(msg.baseRef)
		if ref != "" {
			m.status.BaseRef = ref
		}
		return m, nil
	case spinner.TickMsg:
		cmds := make([]tea.Cmd, 0, 2)
		if m.mode == modeCreating || m.baseRefRefreshing {
			var cmd tea.Cmd
			m.spinner, cmd = m.spinner.Update(msg)
			if cmd != nil {
				cmds = append(cmds, cmd)
			}
		}
		if len(m.ghPendingByBranch) > 0 {
			var cmd tea.Cmd
			m.ghSpinner, cmd = m.ghSpinner.Update(msg)
			if cmd != nil {
				cmds = append(cmds, cmd)
			}
		}
		if len(cmds) == 0 {
			return m, nil
		}
		return m, tea.Batch(cmds...)
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil
	case tea.KeyMsg:
		if m.mode == modeCreating {
			switch msg.String() {
			case "q", "ctrl+c":
				return m, tea.Quit
			}
			return m, nil
		}
		if m.mode == modeDelete {
			switch msg.String() {
			case "y", "Y":
				force := isOrphanedPath(m.status, m.deletePath)
				if err := m.mgr.DeleteWorktree(m.deletePath, force); err != nil {
					m.errMsg = err.Error()
					m.mode = modeList
					return m, nil
				}
				m.mode = modeList
				m.deletePath = ""
				m.deleteBranch = ""
				m.errMsg = ""
				return m, fetchStatusCmd(m.orchestrator)
			case "n", "N", "esc":
				m.mode = modeList
				m.deletePath = ""
				m.deleteBranch = ""
				m.errMsg = ""
				return m, nil
			}
			return m, nil
		}
		if m.mode == modeUnlock {
			switch msg.String() {
			case "y", "Y":
				if err := m.mgr.UnlockWorktree(m.unlockPath); err != nil {
					m.errMsg = err.Error()
					m.mode = modeList
					return m, nil
				}
				m.mode = modeList
				m.unlockPath = ""
				m.unlockBranch = ""
				m.errMsg = ""
				return m, fetchStatusCmd(m.orchestrator)
			case "n", "N", "esc":
				m.mode = modeList
				m.unlockPath = ""
				m.unlockBranch = ""
				m.errMsg = ""
				return m, nil
			}
			return m, nil
		}
		if m.mode == modeBranchName {
			switch msg.String() {
			case "esc":
				m.mode = modeAction
				m.newBranchInput.Blur()
				m.newBranchInput.SetValue("")
				m.errMsg = ""
				return m, nil
			case "enter":
				branch := strings.TrimSpace(m.newBranchInput.Value())
				if branch == "" {
					m.errMsg = "Branch name required."
					return m, nil
				}
				m.mode = modeCreating
				m.creatingBranch = branch
				m.newBranchInput.Blur()
				m.newBranchInput.SetValue("")
				m.errMsg = ""
				return m, tea.Batch(
					m.spinner.Tick,
					createWorktreeCmd(m.mgr, branch, m.status.BaseRef),
				)
			}
			var cmd tea.Cmd
			m.newBranchInput, cmd = m.newBranchInput.Update(msg)
			return m, cmd
		}
		if m.mode == modeAction {
			switch msg.String() {
			case "esc":
				m.mode = modeList
				m.actionIndex = 0
				m.actionBranch = ""
				m.actionCreate = false
				return m, nil
			case "up", "k":
				if m.actionIndex > 0 {
					m.actionIndex--
				}
				return m, nil
			case "down", "j":
				if m.actionIndex < len(currentActionItems(m.actionBranch, m.status.BaseRef, m.actionCreate))-1 {
					m.actionIndex++
				}
				return m, nil
			case "enter":
				if m.actionCreate {
					if m.actionIndex == 0 {
						m.mode = modeBranchName
						m.newBranchInput.SetValue("")
						m.newBranchInput.Focus()
						m.errMsg = ""
						return m, nil
					}
					if m.actionIndex == 1 {
						options, err := availableBranchOptions(m.status, m.mgr)
						if err != nil {
							m.errMsg = err.Error()
							return m, nil
						}
						m.mode = modeBranchPick
						m.branchOptions = options
						m.branchSuggestions = filterBranches(m.branchOptions, "")
						m.branchIndex = 0
						m.branchInput.SetValue("")
						m.branchInput.Focus()
						return m, nil
					}
				}
				if m.actionIndex == 1 {
					m.mode = modeBranchName
					m.newBranchInput.SetValue("")
					m.newBranchInput.Focus()
					m.errMsg = ""
					return m, nil
				}
				if m.actionIndex == 2 {
					options, err := availableBranchOptions(m.status, m.mgr)
					if err != nil {
						m.errMsg = err.Error()
						return m, nil
					}
					m.mode = modeBranchPick
					m.branchOptions = options
					m.branchSuggestions = filterBranches(m.branchOptions, "")
					m.branchIndex = 0
					m.branchInput.SetValue("")
					m.branchInput.Focus()
					return m, nil
				}
				if m.actionIndex == 3 {
					if row, ok := selectedWorktree(m.status, m.listIndex); ok {
						m.errMsg = ""
						m.warnMsg = ""
						m.pendingPath = row.Path
						m.pendingBranch = row.Branch
						m.pendingOpenShell = true
						m.pendingLock = nil
						return m, tea.Quit
					}
				}
				if m.actionIndex == 0 {
					if row, ok := selectedWorktree(m.status, m.listIndex); ok {
						m.errMsg = ""
						m.warnMsg = ""
						lock, err := m.mgr.AcquireWorktreeLock(row.Path)
						if err != nil {
							m.errMsg = err.Error()
							return m, nil
						}
						m.pendingPath = row.Path
						m.pendingBranch = row.Branch
						m.pendingOpenShell = false
						m.pendingLock = lock
						return m, tea.Quit
					}
				}
				m.errMsg = "Not implemented yet."
				m.mode = modeList
				m.actionIndex = 0
				m.actionBranch = ""
				m.actionCreate = false
				return m, nil
			}
			return m, nil
		}
		if m.mode == modeBranchPick {
			switch msg.String() {
			case "esc":
				m.mode = modeAction
				m.branchInput.Blur()
				m.branchSuggestions = nil
				m.branchIndex = 0
				return m, nil
			case "up", "k":
				if m.branchIndex > 0 {
					m.branchIndex--
				}
				return m, nil
			case "down", "j":
				if m.branchIndex < len(m.branchSuggestions)-1 {
					m.branchIndex++
				}
				return m, nil
			case "enter":
				if m.actionCreate {
					branch, ok := selectedBranch(m.branchSuggestions, m.branchIndex)
					if !ok {
						m.errMsg = "Select an existing branch."
						return m, nil
					}
					m.mode = modeCreating
					m.creatingBranch = branch
					m.branchInput.Blur()
					m.branchSuggestions = nil
					m.branchIndex = 0
					m.errMsg = ""
					return m, tea.Batch(
						m.spinner.Tick,
						createWorktreeFromExistingCmd(m.mgr, branch),
					)
				}
				branch, ok := selectedBranch(m.branchSuggestions, m.branchIndex)
				if !ok {
					m.errMsg = "Select an existing branch."
					return m, nil
				}
				row, ok := selectedWorktree(m.status, m.listIndex)
				if !ok {
					m.errMsg = "No worktree selected."
					return m, nil
				}
				lock, err := m.mgr.AcquireWorktreeLock(row.Path)
				if err != nil {
					m.errMsg = err.Error()
					return m, nil
				}
				if err := m.mgr.CheckoutExistingBranch(row.Path, branch); err != nil {
					lock.Release()
					m.errMsg = err.Error()
					return m, nil
				}
				m.errMsg = ""
				m.warnMsg = ""
				m.pendingPath = row.Path
				m.pendingBranch = branch
				m.pendingOpenShell = false
				m.pendingLock = lock
				return m, tea.Quit
			}
			var cmd tea.Cmd
			m.branchInput, cmd = m.branchInput.Update(msg)
			m.branchSuggestions = filterBranches(m.branchOptions, m.branchInput.Value())
			if m.branchIndex >= len(m.branchSuggestions) {
				m.branchIndex = 0
			}
			return m, cmd
		}
		prevPage := m.viewPager.Page
		updatedPager, pagerCmd := m.viewPager.Update(msg)
		m.viewPager = updatedPager
		if m.viewPager.Page != prevPage {
			m.errMsg = ""
			return m, pagerCmd
		}
		switch msg.String() {
		case "q", "ctrl+c":
			return m, tea.Quit
		case "r":
			// Force refresh on demand, including GH enrichment on next status update.
			m.ghLoadedKey = ""
			m.ghFetchingKey = ""
			m.ghPendingByBranch = map[string]bool{}
			m.ghDataByBranch = map[string]PRData{}
			m.ghWarnMsg = ""
			m.forceGHRefresh = true
			return m, fetchStatusCmd(m.orchestrator)
		case "up", "k":
			if m.viewPager.Page == worktreePage {
				if m.listIndex > 0 {
					m.listIndex--
				}
			} else if m.prIndex > 0 {
				m.prIndex--
			}
			return m, nil
		case "down", "j":
			if m.viewPager.Page == worktreePage {
				maxIndex := selectorRowCount(m.status) - 1
				if m.listIndex < maxIndex {
					m.listIndex++
				}
			} else {
				maxIndex := len(m.prs) - 1
				if m.prIndex < maxIndex {
					m.prIndex++
				}
			}
			return m, nil
		case "enter":
			if m.viewPager.Page != worktreePage {
				return m, nil
			}
			if isCreateRow(m.listIndex, m.status) {
				m.mode = modeAction
				m.actionCreate = true
				m.actionBranch = ""
				m.actionIndex = 0
				m.baseRefRefreshing = true
				m.errMsg = ""
				return m, tea.Batch(m.spinner.Tick, refreshBaseRefCmd(m.mgr))
			}
			if row, ok := selectedWorktree(m.status, m.listIndex); ok {
				if isOrphanedPath(m.status, row.Path) {
					m.errMsg = "Cannot open actions for orphaned worktree."
					return m, nil
				}
				if !row.Available {
					m.errMsg = "Worktree is currently in use."
					return m, nil
				}
				m.mode = modeAction
				m.actionCreate = false
				m.actionBranch = row.Branch
				m.actionIndex = 0
				m.baseRefRefreshing = true
				m.errMsg = ""
				return m, tea.Batch(m.spinner.Tick, refreshBaseRefCmd(m.mgr))
			}
		case "s":
			if m.viewPager.Page != worktreePage {
				return m, nil
			}
			if row, ok := selectedWorktree(m.status, m.listIndex); ok {
				if isOrphanedPath(m.status, row.Path) {
					m.errMsg = "Cannot open shell for orphaned worktree."
					return m, nil
				}
				m.errMsg = ""
				m.warnMsg = ""
				m.pendingPath = row.Path
				m.pendingBranch = row.Branch
				m.pendingOpenShell = true
				m.pendingLock = nil
				return m, tea.Quit
			}
		case "d":
			if m.viewPager.Page != worktreePage {
				return m, nil
			}
			if row, ok := selectedWorktree(m.status, m.listIndex); ok {
				m.mode = modeDelete
				m.deletePath = row.Path
				m.deleteBranch = row.Branch
				m.errMsg = ""
				return m, nil
			}
		case "p", "P":
			if m.viewPager.Page == prsPage {
				pr, ok := selectedPR(m.prs, m.prIndex)
				if !ok {
					m.errMsg = "No PR selected."
					return m, nil
				}
				if strings.TrimSpace(pr.URL) == "" {
					m.errMsg = "No URL for selected PR."
					return m, nil
				}
				if err := m.runner.OpenURL(pr.URL); err != nil {
					m.errMsg = err.Error()
					return m, nil
				}
				m.errMsg = ""
				return m, nil
			}
			if row, ok := selectedWorktree(m.status, m.listIndex); ok {
				if strings.TrimSpace(row.PRURL) == "" {
					m.errMsg = "No PR URL for selected worktree."
					return m, nil
				}
				if err := m.runner.OpenURL(row.PRURL); err != nil {
					m.errMsg = err.Error()
					return m, nil
				}
				m.errMsg = ""
				return m, nil
			}
		case "u":
			if m.viewPager.Page != worktreePage {
				return m, nil
			}
			if row, ok := selectedWorktree(m.status, m.listIndex); ok {
				if isOrphanedPath(m.status, row.Path) {
					m.errMsg = "Cannot unlock orphaned worktree."
					return m, nil
				}
				if row.Available {
					m.errMsg = "Worktree is not locked."
					return m, nil
				}
				m.mode = modeUnlock
				m.unlockPath = row.Path
				m.unlockBranch = row.Branch
				m.errMsg = ""
				return m, nil
			}
		}
	}
	return m, nil
}

func (m model) View() string {
	var b strings.Builder
	showTopBar := m.ready && m.status.InRepo && m.mode == modeList
	if showTopBar {
		b.WriteString(bannerStyle.Render("WTX"))
		b.WriteString("  ")
		b.WriteString(renderViewHeader(m.viewPager.Page))
		b.WriteString("\n\n")
	} else {
		b.WriteString(bannerStyle.Render("WTX"))
		b.WriteString("\n\n")
	}

	if !m.ready {
		b.WriteString("Loading...\n")
		return b.String()
	}

	if !m.status.GitInstalled {
		b.WriteString(errorStyle.Render("Git not installed."))
		b.WriteString("\n")
		b.WriteString("Install git to use wtx.\n")
		b.WriteString("\n")
		b.WriteString("Press q to quit.\n")
		return b.String()
	}

	if !m.status.InRepo {
		b.WriteString(errorStyle.Render("Not inside a git repository."))
		b.WriteString("\n")
		if m.status.CWD != "" {
			b.WriteString(fmt.Sprintf("CWD: %s\n", m.status.CWD))
		}
		b.WriteString("\n")
		b.WriteString("Press q to quit.\n")
		return b.String()
	}

	if m.mode == modeAction {
		title := "Worktree actions:"
		if m.actionCreate {
			title = "New worktree actions:"
		}
		b.WriteString(title + "\n")
			if m.baseRefRefreshing {
				b.WriteString("  " + secondaryStyle.Render(m.spinner.View()+" Refreshing base branch...") + "\n")
			}
		for i, item := range currentActionItems(m.actionBranch, m.status.BaseRef, m.actionCreate) {
			line := "  " + actionNormalStyle.Render(item)
			if i == m.actionIndex {
				line = "  " + actionSelectedStyle.Render(item)
			}
			b.WriteString(line + "\n")
		}
		if m.errMsg != "" {
			b.WriteString("\n")
			b.WriteString(errorStyle.Render(m.errMsg))
			b.WriteString("\n")
		}
		b.WriteString("\nPress enter to select, esc to cancel.\n")
		return b.String()
	}
	if m.mode == modeBranchName {
		title := "New branch name:"
		if m.actionCreate {
			title = "New worktree branch:"
		}
		b.WriteString(title + "\n")
		b.WriteString(inputStyle.Render(m.newBranchInput.View()))
		b.WriteString("\n")
		if m.errMsg != "" {
			b.WriteString(errorStyle.Render(m.errMsg))
			b.WriteString("\n")
		}
		b.WriteString("\nPress enter to create, esc to cancel.\n")
		return b.String()
	}
	if m.mode == modeBranchPick {
		b.WriteString("Choose an existing branch:\n")
		b.WriteString(inputStyle.Render(m.branchInput.View()))
		b.WriteString("\n")
		for i, suggestion := range m.branchSuggestions {
			line := "  " + actionNormalStyle.Render(suggestion)
			if i == m.branchIndex {
				line = "  " + actionSelectedStyle.Render(suggestion)
			}
			b.WriteString(line + "\n")
		}
		if m.errMsg != "" {
			b.WriteString("\n")
			b.WriteString(errorStyle.Render(m.errMsg))
			b.WriteString("\n")
		}
		b.WriteString("\nPress enter to select, esc to cancel.\n")
		return b.String()
	}
	if m.mode == modeDelete {
		b.WriteString("Delete worktree:\n")
		b.WriteString(fmt.Sprintf("%s\n", m.deleteBranch))
		b.WriteString(fmt.Sprintf("%s\n", m.deletePath))
		if m.errMsg != "" {
			b.WriteString("\n")
			b.WriteString(errorStyle.Render(m.errMsg))
			b.WriteString("\n")
		}
		b.WriteString("\nAre you sure? (y/N)\n")
		return b.String()
	}
	if m.mode == modeUnlock {
		b.WriteString("Unlock worktree:\n")
		b.WriteString(fmt.Sprintf("%s\n", m.unlockBranch))
		b.WriteString(fmt.Sprintf("%s\n", m.unlockPath))
		if m.errMsg != "" {
			b.WriteString("\n")
			b.WriteString(errorStyle.Render(m.errMsg))
			b.WriteString("\n")
		}
		b.WriteString("\nAre you sure? (y/N)\n")
		return b.String()
	}
	if m.viewPager.Page == prsPage {
		prLoading := strings.TrimSpace(m.ghFetchingKey) != ""
		b.WriteString(baseStyle.Render(renderPRSelector(m.prs, m.prIndex, prLoading, m.ghSpinner.View())))
	} else {
		b.WriteString(baseStyle.Render(renderSelector(m.status, m.listIndex, m.ghPendingByBranch, m.ghSpinner.View())))
	}
	b.WriteString("\n")
	if m.status.Err != nil {
		b.WriteString(errorStyle.Render(fmt.Sprintf("Error: %v", m.status.Err)))
		b.WriteString("\n")
	}
	if m.errMsg != "" {
		b.WriteString(errorStyle.Render(m.errMsg))
		b.WriteString("\n")
	}
	if m.mode == modeCreating {
		b.WriteString("\n")
		b.WriteString(m.spinner.View())
		b.WriteString(" Creating ")
		b.WriteString(branchStyle.Render(m.creatingBranch))
		b.WriteString("...\n")
	}
	if m.warnMsg != "" {
		b.WriteString(warnStyle.Render(m.warnMsg))
		b.WriteString("\n")
	}
	if m.ghWarnMsg != "" {
		b.WriteString(warnStyle.Render(m.ghWarnMsg))
		b.WriteString("\n")
	}
	if len(m.status.Malformed) > 0 {
		b.WriteString("\nMalformed entries:\n")
		for _, line := range m.status.Malformed {
			b.WriteString(" - ")
			b.WriteString(line)
			b.WriteString("\n")
		}
	}
	if m.viewPager.Page == worktreePage {
		selectedPath := currentWorktreePath(m.status, m.listIndex)
		if selectedPath != "" {
			b.WriteString("\n")
			b.WriteString(secondaryStyle.Render(selectedPath))
			b.WriteString("\n")
		}
	}
	if m.viewPager.Page == prsPage {
		if pr, ok := selectedPR(m.prs, m.prIndex); ok && strings.TrimSpace(pr.URL) != "" {
			b.WriteString("\n")
			b.WriteString(secondaryStyle.Render(pr.URL))
			b.WriteString("\n")
		}
	}
	b.WriteString("\n")
	help := "Press ←/→ to switch views, r to refresh, q to quit."
	if m.viewPager.Page == prsPage {
		help = "Press up/down to select PR, p to open URL, ←/→ to switch views, r to refresh, q to quit."
	} else if m.mode == modeCreating {
		help = "Creating worktree..."
	} else if isCreateRow(m.listIndex, m.status) {
		help = "Press enter for actions, ←/→ to switch views, r to refresh, q to quit."
		} else if wt, ok := selectedWorktree(m.status, m.listIndex); ok {
			prHint := ""
			if strings.TrimSpace(wt.PRURL) != "" {
				prHint = ", p to open PR"
			}
			if !wt.Available && !isOrphanedPath(m.status, wt.Path) {
				help = "Press u to unlock, d to delete" + prHint + ", ←/→ to switch views, r to refresh, q to quit."
			} else {
				help = "Press enter for actions, s for shell, d to delete" + prHint + ", ←/→ to switch views, r to refresh, q to quit."
			}
		}
	b.WriteString(help + "\n")
	return b.String()
}

func renderViewHeader(page int) string {
	tabs := []string{
		"Worktrees",
		"PRs",
	}
	activeTabStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("252"))
	inactiveTabStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("244"))
	for i := range tabs {
		if i == page {
			tabs[i] = activeTabStyle.Render(tabs[i])
		} else {
			tabs[i] = inactiveTabStyle.Render(tabs[i])
		}
	}
	return strings.Join(tabs, " | ") + "  " + subtleHintStyle.Render("← → change view")
}

func renderPRSelector(prs []PRListData, cursor int, loading bool, loadingGlyph string) string {
	const (
		numberWidth   = 8
		branchWidth   = 24
		titleWidth    = 48
		ciWidth       = 12
		approvalWidth = 14
		statusWidth   = 10
	)
	var b strings.Builder
	header := formatOpenPRLine("PR", "Branch", "Title", "CI status", "Approval", "PR status", numberWidth, branchWidth, titleWidth, ciWidth, approvalWidth, statusWidth)
	b.WriteString(selectorHeaderStyle.Render("  " + header))
	b.WriteString("\n")
	if len(prs) == 0 {
		b.WriteString("  ")
		b.WriteString(selectorDisabledStyle.Render("No PRs."))
		if loading {
			b.WriteString("\n  ")
			b.WriteString(secondaryStyle.Render(loadingGlyph + " Loading PRs..."))
		}
		return b.String()
	}
	for i, pr := range prs {
		rowStyle := selectorNormalStyle
		rowSelectedStyle := selectorSelectedStyle
		if isInactivePRStatus(pr.Status) {
			rowStyle = selectorDisabledStyle
			rowSelectedStyle = selectorDisabledSelectedStyle
		}
		line := formatOpenPRLine(
			fmt.Sprintf("#%d", pr.Number),
			pr.Branch,
			pr.Title,
			formatPRListCI(pr),
			formatPRListApproval(pr),
			formatPRListStatus(pr),
			numberWidth,
			branchWidth,
			titleWidth,
			ciWidth,
			approvalWidth,
			statusWidth,
		)
		if i == cursor {
			b.WriteString("  " + rowSelectedStyle.Render(line))
		} else {
			b.WriteString("  " + rowStyle.Render(line))
		}
		b.WriteString("\n")
	}
	if loading {
		b.WriteString("  ")
		b.WriteString(secondaryStyle.Render(loadingGlyph + " Loading PRs..."))
	}
	return b.String()
}

func formatOpenPRLine(number string, branch string, title string, ci string, approval string, status string, numberWidth int, branchWidth int, titleWidth int, ciWidth int, approvalWidth int, statusWidth int) string {
	return padOrTrim(number, numberWidth) + " " +
		padOrTrim(branch, branchWidth) + " " +
		padOrTrim(title, titleWidth) + " " +
		padOrTrim(ci, ciWidth) + " " +
		padOrTrim(approval, approvalWidth) + " " +
		padOrTrim(status, statusWidth)
}

func formatPRListCI(pr PRListData) string {
	if pr.CITotal == 0 {
		return "-"
	}
	switch pr.CIState {
	case PRCISuccess:
		return fmt.Sprintf("✓ %d/%d", pr.CICompleted, pr.CITotal)
	case PRCIFail:
		return fmt.Sprintf("✗ %d/%d", pr.CICompleted, pr.CITotal)
	case PRCIInProgress:
		return fmt.Sprintf("… %d/%d", pr.CICompleted, pr.CITotal)
	default:
		return "-"
	}
}

func formatPRListApproval(pr PRListData) string {
	if pr.Approved {
		return "approved"
	}
	switch strings.ToLower(strings.TrimSpace(pr.ReviewDecision)) {
	case "changes_requested":
		return "changes_requested"
	case "review_required":
		return "review_required"
	default:
		return "-"
	}
}

func formatPRListStatus(pr PRListData) string {
	status := strings.TrimSpace(strings.ToLower(pr.Status))
	if status == "" {
		return "-"
	}
	return status
}

func isInactivePRStatus(status string) bool {
	s := strings.TrimSpace(strings.ToLower(status))
	return s == "closed" || s == "merged"
}

func selectedPR(prs []PRListData, index int) (PRListData, bool) {
	if index < 0 || index >= len(prs) {
		return PRListData{}, false
	}
	return prs[index], true
}

func clampPRIndex(index int, prs []PRListData) int {
	if len(prs) == 0 {
		return 0
	}
	if index < 0 {
		return 0
	}
	if index >= len(prs) {
		return len(prs) - 1
	}
	return index
}

type statusMsg WorktreeStatus
type pollStatusTickMsg time.Time
type ghDataMsg struct {
	repoRoot string
	key      string
	byBranch map[string]PRData
	prs      []PRListData
	err      error
}
type baseRefResolvedMsg struct {
	baseRef string
}
type createWorktreeDoneMsg struct {
	created WorktreeInfo
	err     error
}

func fetchStatusCmd(orchestrator *WorktreeOrchestrator) tea.Cmd {
	return func() tea.Msg {
		if orchestrator == nil {
			return statusMsg(WorktreeStatus{})
		}
		return statusMsg(orchestrator.Status())
	}
}

func pollStatusTickCmd() tea.Cmd {
	return tea.Tick(time.Second, func(t time.Time) tea.Msg {
		return pollStatusTickMsg(t)
	})
}

func fetchGHDataCmd(orchestrator *WorktreeOrchestrator, status WorktreeStatus, key string, force bool) tea.Cmd {
	return func() tea.Msg {
		var byBranch map[string]PRData
		var prs []PRListData
		var byBranchErr error
		var prListErr error
		if orchestrator == nil {
			byBranch = map[string]PRData{}
			prs = []PRListData{}
		} else {
			byBranch, byBranchErr = orchestrator.PRDataForStatusWithError(status, force)
			prs, prListErr = orchestrator.PRsForStatusWithError(status, force)
			if byBranch == nil {
				byBranch = map[string]PRData{}
			}
			if prs == nil {
				prs = []PRListData{}
			}
		}
		err := byBranchErr
		if err == nil {
			err = prListErr
		}
		return ghDataMsg{
			repoRoot: status.RepoRoot,
			key:      key,
			byBranch: byBranch,
			prs:      prs,
			err:      err,
		}
	}
}

func createWorktreeCmd(mgr *WorktreeManager, branch string, baseRef string) tea.Cmd {
	return func() tea.Msg {
		created, err := mgr.CreateWorktree(branch, baseRef)
		return createWorktreeDoneMsg{created: created, err: err}
	}
}

func createWorktreeFromExistingCmd(mgr *WorktreeManager, branch string) tea.Cmd {
	return func() tea.Msg {
		created, err := mgr.CreateWorktreeFromBranch(branch)
		return createWorktreeDoneMsg{created: created, err: err}
	}
}

func refreshBaseRefCmd(mgr *WorktreeManager) tea.Cmd {
	return func() tea.Msg {
		if mgr == nil {
			return baseRefResolvedMsg{baseRef: "main"}
		}
		return baseRefResolvedMsg{baseRef: mgr.ResolveBaseRefForNewBranch()}
	}
}

func renderSelector(status WorktreeStatus, cursor int, pendingByBranch map[string]bool, loadingGlyph string) string {
	if !status.InRepo {
		return ""
	}
	const (
		branchWidth   = 40
		prWidth       = 12
		ciWidth       = 12
		approvedWidth = 12
		prStateWidth  = 10
	)
	var b strings.Builder
	header := formatSelectorLine("Branch", "PR", "CI", "Approved", "PR Status", branchWidth, prWidth, ciWidth, approvedWidth, prStateWidth)
	b.WriteString(selectorHeaderStyle.Render("  " + header))
	b.WriteString("\n")
	orphaned := make(map[string]bool, len(status.Orphaned))
	for _, wt := range status.Orphaned {
		orphaned[wt.Path] = true
	}
	worktrees := worktreesForDisplay(status)
	for i, wt := range worktrees {
		label := wt.Branch
		rowStyle := selectorNormalStyle
		rowSelectedStyle := selectorSelectedStyle
		if orphaned[wt.Path] {
			label = fmt.Sprintf("%s (orphaned)", wt.Branch)
			rowStyle = selectorDisabledStyle
			rowSelectedStyle = selectorDisabledSelectedStyle
		} else if !wt.Available {
			label = wt.Branch + " (locked)"
			rowStyle = selectorDisabledStyle
			rowSelectedStyle = selectorDisabledSelectedStyle
		}
		pending := pendingByBranch[strings.TrimSpace(wt.Branch)]
		line := formatSelectorLine(
			label,
			formatPRLabel(wt, pending, loadingGlyph),
			formatCILabel(wt, pending, loadingGlyph),
			formatReviewLabel(wt, pending, loadingGlyph),
			formatPRStatusLabel(wt, pending, loadingGlyph),
			branchWidth,
			prWidth,
			ciWidth,
			approvedWidth,
			prStateWidth,
		)
		if i == cursor {
			b.WriteString("  " + rowSelectedStyle.Render(line))
		} else {
			b.WriteString("  " + rowStyle.Render(line))
		}
		b.WriteString("\n")
	}
	createIdx := len(worktrees)
	createLine := formatSelectorLine("+ New worktree", "", "", "", "", branchWidth, prWidth, ciWidth, approvedWidth, prStateWidth)
	if createIdx == cursor {
		b.WriteString("  " + selectorSelectedStyle.Render(createLine))
	} else {
		b.WriteString("  " + selectorNormalStyle.Render(createLine))
	}
	return b.String()
}

func formatSelectorLine(branch string, pr string, ci string, approved string, prState string, branchWidth int, prWidth int, ciWidth int, approvedWidth int, prStateWidth int) string {
	return padOrTrim(branch, branchWidth) + " " +
		padOrTrim(pr, prWidth) + " " +
		padOrTrim(ci, ciWidth) + " " +
		padOrTrim(approved, approvedWidth) + " " +
		padOrTrim(prState, prStateWidth)
}

func padOrTrim(s string, width int) string {
	if width <= 0 {
		return ""
	}
	r := []rune(s)
	if len(r) > width {
		if width <= 3 {
			return string(r[:width])
		}
		return string(r[:width-3]) + "..."
	}
	if len(r) < width {
		return s + strings.Repeat(" ", width-len(r))
	}
	return s
}

var (
	baseStyle   = lipgloss.NewStyle()
	bannerStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#FFF7DB")).
			Background(lipgloss.Color("#7D56F4")).
			Padding(0, 1)
	errorStyle            = lipgloss.NewStyle().Foreground(lipgloss.Color("1")).Bold(true)
	secondaryStyle        = lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
	actionNormalStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("251"))
	actionSelectedStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("#7D56F4")).Bold(true)
	selectorNormalStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("251"))
	selectorSelectedStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#7D56F4")).
				Bold(true)
	selectorDisabledStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("241"))
	selectorDisabledSelectedStyle = lipgloss.NewStyle().
					Foreground(lipgloss.Color("#7D56F4")).
					Bold(true)
	selectorHeaderStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("15")).Bold(true)
	branchStyle         = lipgloss.NewStyle().Foreground(lipgloss.Color("15")).Bold(true)
	branchInlineStyle   = lipgloss.NewStyle().Bold(true)
	warnStyle           = lipgloss.NewStyle().Foreground(lipgloss.Color("3")).Bold(true)
	subtleHintStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("239"))
	inputStyle          = lipgloss.NewStyle().
				Padding(0, 1)
)

func max(a int, b int) int {
	if a > b {
		return a
	}
	return b
}

type uiMode int

const (
	worktreePage = 0
	prsPage      = 1
)

const (
	modeList uiMode = iota
	modeCreating
	modeDelete
	modeUnlock
	modeAction
	modeBranchName
	modeBranchPick
)

func newViewPager() paginator.Model {
	p := paginator.New(paginator.WithTotalPages(2))
	return p
}

func newBranchInput() textinput.Model {
	ti := textinput.New()
	ti.Placeholder = "branch name"
	ti.CharLimit = 200
	ti.Width = 40
	return ti
}

func newCreateBranchInput() textinput.Model {
	ti := textinput.New()
	ti.Placeholder = "feature/my-branch"
	ti.CharLimit = 200
	ti.Width = 40
	return ti
}

func isCreateRow(cursor int, status WorktreeStatus) bool {
	if !status.InRepo {
		return false
	}
	if cursor < 0 {
		return false
	}
	return cursor == len(worktreesForDisplay(status))
}

func selectedWorktree(status WorktreeStatus, cursor int) (WorktreeInfo, bool) {
	if !status.InRepo {
		return WorktreeInfo{}, false
	}
	worktrees := worktreesForDisplay(status)
	if cursor < 0 || cursor >= len(worktrees) {
		return WorktreeInfo{}, false
	}
	return worktrees[cursor], true
}

func isOrphanedPath(status WorktreeStatus, path string) bool {
	for _, wt := range status.Orphaned {
		if wt.Path == path {
			return true
		}
	}
	return false
}

func actionItems(branch string, baseRef string) []string {
	base := strings.TrimSpace(baseRef)
	if base == "" {
		base = "main"
	}
	return []string{
		"Use " + branchInlineStyle.Render(branch),
		"Checkout new branch from " + branchInlineStyle.Render(base),
		"Choose an existing branch",
		"Open shell here",
	}
}

func createActionItems(baseRef string) []string {
	base := strings.TrimSpace(baseRef)
	if base == "" {
		base = "main"
	}
	return []string{
		"Checkout new branch from " + branchInlineStyle.Render(base),
		"Choose an existing branch",
	}
}

func currentActionItems(branch string, baseRef string, create bool) []string {
	if create {
		return createActionItems(baseRef)
	}
	return actionItems(branch, baseRef)
}

func currentWorktreePath(status WorktreeStatus, cursor int) string {
	wt, ok := selectedWorktree(status, cursor)
	if !ok {
		return ""
	}
	return wt.Path
}

func findWorktreeByPath(status WorktreeStatus, path string) (int, WorktreeInfo, bool) {
	needle := strings.TrimSpace(path)
	if needle == "" {
		return 0, WorktreeInfo{}, false
	}
	worktrees := worktreesForDisplay(status)
	for i, wt := range worktrees {
		if strings.TrimSpace(wt.Path) == needle {
			return i, wt, true
		}
	}
	return 0, WorktreeInfo{}, false
}

func greenCheck() string {
	return "✓"
}

func redX() string {
	return "✗"
}

func formatPRLabel(wt WorktreeInfo, pending bool, loadingGlyph string) string {
	if pending {
		return loadingGlyph
	}
	if !wt.HasPR || wt.PRNumber <= 0 {
		return "-"
	}
	return fmt.Sprintf("#%d", wt.PRNumber)
}

func formatPRStatusLabel(wt WorktreeInfo, pending bool, loadingGlyph string) string {
	if pending {
		return loadingGlyph
	}
	if !wt.HasPR {
		return "-"
	}
	status := strings.TrimSpace(strings.ToLower(wt.PRStatus))
	if status == "" {
		return "-"
	}
	switch status {
	case "draft", "open", "closed", "merged":
		return status
	default:
		return "-"
	}
}

func formatCILabel(wt WorktreeInfo, pending bool, loadingGlyph string) string {
	if pending {
		return loadingGlyph
	}
	if !wt.HasPR || wt.CITotal == 0 {
		return "-"
	}
	switch wt.CIState {
	case PRCISuccess:
		return fmt.Sprintf("✓ %d/%d", wt.CIDone, wt.CITotal)
	case PRCIFail:
		return fmt.Sprintf("✗ %d/%d", wt.CIDone, wt.CITotal)
	case PRCIInProgress:
		return fmt.Sprintf("… %d/%d", wt.CIDone, wt.CITotal)
	default:
		return "-"
	}
}

func formatReviewLabel(wt WorktreeInfo, pending bool, loadingGlyph string) string {
	if pending {
		return loadingGlyph
	}
	if !wt.HasPR {
		return "-"
	}
	prefix := "○"
	if wt.Approved {
		prefix = "✓"
	}
	return fmt.Sprintf("%s u:%d", prefix, wt.UnresolvedComments)
}

func uniqueBranches(status WorktreeStatus) []string {
	seen := make(map[string]bool)
	out := make([]string, 0, len(status.Worktrees)+1)
	for _, wt := range status.Worktrees {
		name := strings.TrimSpace(wt.Branch)
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		out = append(out, name)
	}
	if !seen["main"] {
		out = append(out, "main")
	}
	return out
}

func filterBranches(options []string, query string) []string {
	q := strings.ToLower(strings.TrimSpace(query))
	if q == "" {
		return options
	}
	out := make([]string, 0, len(options))
	for _, opt := range options {
		if strings.Contains(strings.ToLower(opt), q) {
			out = append(out, opt)
		}
	}
	return out
}

func availableBranchOptions(status WorktreeStatus, mgr *WorktreeManager) ([]string, error) {
	options, err := mgr.ListLocalBranchesByRecentUse()
	if err != nil {
		return nil, err
	}
	inUse := make(map[string]bool, len(status.Worktrees))
	for _, wt := range status.Worktrees {
		name := strings.TrimSpace(wt.Branch)
		if name == "" {
			continue
		}
		inUse[name] = true
	}
	filtered := make([]string, 0, len(options))
	for _, opt := range options {
		if inUse[opt] {
			continue
		}
		filtered = append(filtered, opt)
	}
	if len(filtered) == 0 {
		return nil, fmt.Errorf("no available branches (all branches currently in use)")
	}
	return filtered, nil
}

func selectedBranch(suggestions []string, index int) (string, bool) {
	if index < 0 || index >= len(suggestions) {
		return "", false
	}
	value := strings.TrimSpace(suggestions[index])
	return value, value != ""
}

func selectorRowCount(status WorktreeStatus) int {
	if !status.InRepo {
		return 0
	}
	return len(worktreesForDisplay(status)) + 1
}

func pendingBranchesByName(status WorktreeStatus) map[string]bool {
	out := make(map[string]bool, len(status.Worktrees))
	for _, wt := range status.Worktrees {
		name := strings.TrimSpace(wt.Branch)
		if name == "" {
			continue
		}
		out[name] = true
	}
	return out
}

func ghDataKeyForStatus(status WorktreeStatus) string {
	repo := strings.TrimSpace(status.RepoRoot)
	if repo == "" || !status.InRepo {
		return ""
	}
	branches := make([]string, 0, len(status.Worktrees))
	seen := make(map[string]bool, len(status.Worktrees))
	for _, wt := range status.Worktrees {
		name := strings.TrimSpace(wt.Branch)
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		branches = append(branches, name)
	}
	sort.Strings(branches)
	return repo + "|" + strings.Join(branches, ",")
}

func ghWarningFromErr(err error) string {
	if err == nil {
		return ""
	}
	msg := strings.ToLower(strings.TrimSpace(err.Error()))
	switch {
	case strings.Contains(msg, "executable file not found"),
		strings.Contains(msg, "no such file or directory"),
		strings.Contains(msg, "gh: command not found"):
		return "GitHub CLI not available. Install `gh` to show PR/CI/review."
	case strings.Contains(msg, "gh auth login"),
		strings.Contains(msg, "not logged"),
		strings.Contains(msg, "authentication"),
		strings.Contains(msg, "http 401"),
		strings.Contains(msg, "requires authentication"):
		return "GitHub CLI not authenticated. Run `gh auth login`."
	default:
		return "GitHub data unavailable right now."
	}
}

func worktreesForDisplay(status WorktreeStatus) []WorktreeInfo {
	if !status.InRepo {
		return nil
	}
	orphaned := make(map[string]bool, len(status.Orphaned))
	for _, wt := range status.Orphaned {
		orphaned[wt.Path] = true
	}
	out := make([]WorktreeInfo, len(status.Worktrees))
	copy(out, status.Worktrees)
	sort.SliceStable(out, func(i, j int) bool {
		iFree := out[i].Available && !orphaned[out[i].Path]
		jFree := out[j].Available && !orphaned[out[j].Path]
		if iFree != jFree {
			return iFree
		}
		if iFree && jFree {
			iLastUsed := out[i].LastUsedUnix
			jLastUsed := out[j].LastUsedUnix
			if iLastUsed != jLastUsed {
				return iLastUsed > jLastUsed
			}
		}
		iBranch := strings.ToLower(strings.TrimSpace(out[i].Branch))
		jBranch := strings.ToLower(strings.TrimSpace(out[j].Branch))
		if iBranch != jBranch {
			return iBranch > jBranch
		}
		return out[i].Path > out[j].Path
	})
	return out
}

func applyPRDataToStatus(status *WorktreeStatus, byBranch map[string]PRData) {
	if status == nil {
		return
	}
	for i := range status.Worktrees {
		b := strings.TrimSpace(status.Worktrees[i].Branch)
		status.Worktrees[i].HasPR = false
		status.Worktrees[i].PRNumber = 0
		status.Worktrees[i].PRURL = ""
		status.Worktrees[i].PRStatus = ""
		status.Worktrees[i].CIState = PRCINone
		status.Worktrees[i].CIDone = 0
		status.Worktrees[i].CITotal = 0
		status.Worktrees[i].Approved = false
		status.Worktrees[i].UnresolvedComments = 0
		if b == "" {
			continue
		}
		if pr, ok := byBranch[b]; ok {
			status.Worktrees[i].HasPR = true
			status.Worktrees[i].PRNumber = pr.Number
			status.Worktrees[i].PRURL = pr.URL
			status.Worktrees[i].PRStatus = pr.Status
			status.Worktrees[i].CIState = pr.CIState
			status.Worktrees[i].CIDone = pr.CICompleted
			status.Worktrees[i].CITotal = pr.CITotal
			status.Worktrees[i].Approved = pr.Approved
			status.Worktrees[i].UnresolvedComments = pr.UnresolvedComments
		}
	}
}

func clampListIndex(index int, status WorktreeStatus) int {
	maxIndex := selectorRowCount(status) - 1
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

func newSpinner() spinner.Model {
	s := spinner.New()
	s.Spinner = spinner.Dot
	s.Style = lipgloss.NewStyle().Foreground(lipgloss.Color("#7D56F4"))
	return s
}

func newGHSpinner() spinner.Model {
	s := spinner.New()
	s.Spinner = spinner.Dot
	return s
}
