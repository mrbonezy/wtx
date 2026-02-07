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
	uiview "github.com/mrbonezy/wtx/ui"
)

type model struct {
	mgr                  *WorktreeManager
	orchestrator         *WorktreeOrchestrator
	runner               *Runner
	status               WorktreeStatus
	listIndex            int
	prIndex              int
	prs                  []PRListData
	viewPager            paginator.Model
	ready                bool
	width                int
	height               int
	mode                 uiMode
	branchInput          textinput.Model
	newBranchInput       textinput.Model
	prSearchInput        textinput.Model
	prSearchActive       bool
	spinner              spinner.Model
	ghSpinner            spinner.Model
	ghPendingByBranch    map[string]bool
	ghDataByBranch       map[string]PRData
	ghLoadedKey          string
	ghFetchingKey        string
	prListLoadedRepo     string
	prListFetchingRepo   string
	prListEnrichingRepo  string
	forcePREnrichRefresh bool
	forceGHRefresh       bool
	ghWarnMsg            string
	errMsg               string
	warnMsg              string
	creatingBranch       string
	deletePath           string
	deleteBranch         string
	unlockPath           string
	unlockBranch         string
	actionBranch         string
	actionIndex          int
	actionCreate         bool
	branchOptions        []string
	branchSuggestions    []string
	branchIndex          int
	pendingPath          string
	pendingBranch        string
	pendingOpenShell     bool
	pendingLock          *WorktreeLock
	autoActionPath       string
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
	m.prSearchInput = newPRSearchInput()
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
		m.prIndex = clampPRIndex(m.prIndex, filteredPRs(m.prs, m.prSearchInput.Value()))
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
			m.prListLoadedRepo = ""
			m.prListFetchingRepo = ""
			m.prListEnrichingRepo = ""
			m.ghWarnMsg = ""
			return m, nil
		}
		if key == m.ghLoadedKey || key == m.ghFetchingKey {
			applyPRDataToStatus(&m.status, m.ghDataByBranch)
		}
		repo := strings.TrimSpace(m.status.RepoRoot)
		fetchByBranch := key != m.ghLoadedKey && key != m.ghFetchingKey
		fetchPRList := m.viewPager.Page == prsPage &&
			repo != "" &&
			repo != m.prListLoadedRepo &&
			repo != m.prListFetchingRepo
		if !fetchByBranch && !fetchPRList {
			// Local status polling runs every second; avoid re-fetching GH unless needed.
			return m, nil
		}
		if fetchByBranch {
			m.ghFetchingKey = key
			m.ghPendingByBranch = pendingBranchesByName(m.status)
		}
		if fetchPRList {
			m.prListFetchingRepo = repo
		}
		force := m.forceGHRefresh
		m.forceGHRefresh = false
		cmd := fetchGHDataCmd(m.orchestrator, m.status, key, force, fetchByBranch, fetchPRList, false)
		return m, tea.Batch(cmd, m.ghSpinner.Tick)
	case ghDataMsg:
		if strings.TrimSpace(msg.repoRoot) == "" || strings.TrimSpace(m.status.RepoRoot) == "" {
			return m, nil
		}
		if msg.repoRoot != m.status.RepoRoot {
			return m, nil
		}
		if msg.fetchedByBranch {
			if strings.TrimSpace(msg.key) == "" || msg.key != m.ghFetchingKey {
				msg.fetchedByBranch = false
			}
		}
		if msg.fetchedPRList {
			if msg.repoRoot != m.prListFetchingRepo {
				msg.fetchedPRList = false
			}
		}
		if msg.fetchedPRListEnriched {
			if msg.repoRoot != m.prListEnrichingRepo {
				msg.fetchedPRListEnriched = false
			}
		}
		if !msg.fetchedByBranch && !msg.fetchedPRList && !msg.fetchedPRListEnriched {
			// Ignore stale GH responses that raced with newer fetches.
			return m, nil
		}
		m.ghWarnMsg = ghWarningFromErr(msg.err)
		if msg.fetchedByBranch {
			m.ghDataByBranch = msg.byBranch
			applyPRDataToStatus(&m.status, m.ghDataByBranch)
			m.ghPendingByBranch = map[string]bool{}
			m.ghLoadedKey = msg.key
			m.ghFetchingKey = ""
		}
		if msg.fetchedPRList {
			m.prs = msg.prs
			m.prListLoadedRepo = msg.repoRoot
			m.prListFetchingRepo = ""
			if m.viewPager.Page == prsPage && m.prListEnrichingRepo == "" {
				m.prListEnrichingRepo = msg.repoRoot
				forceEnrich := m.forcePREnrichRefresh
				m.forcePREnrichRefresh = false
				return m, tea.Batch(
					fetchGHDataCmd(m.orchestrator, m.status, m.ghFetchingKey, forceEnrich, false, false, true),
					m.ghSpinner.Tick,
				)
			}
		}
		if msg.fetchedPRListEnriched {
			m.prs = msg.prs
			m.prListLoadedRepo = msg.repoRoot
			m.prListEnrichingRepo = ""
		}
		m.prIndex = clampPRIndex(m.prIndex, filteredPRs(m.prs, m.prSearchInput.Value()))
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
	case spinner.TickMsg:
		cmds := make([]tea.Cmd, 0, 2)
		if m.mode == modeCreating {
			var cmd tea.Cmd
			m.spinner, cmd = m.spinner.Update(msg)
			if cmd != nil {
				cmds = append(cmds, cmd)
			}
		}
		if len(m.ghPendingByBranch) > 0 || strings.TrimSpace(m.prListFetchingRepo) != "" || strings.TrimSpace(m.prListEnrichingRepo) != "" {
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
						options, err := availableBranchOptions(m.status, m.mgr, true)
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
					options, err := availableBranchOptions(m.status, m.mgr, false)
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
					if wt, reusable, reason := reusableWorktreeForBranch(m.status, branch); reusable {
						lock, err := m.mgr.AcquireWorktreeLock(wt.Path)
						if err != nil {
							m.errMsg = err.Error()
							return m, nil
						}
						m.errMsg = ""
						m.warnMsg = ""
						m.pendingPath = wt.Path
						m.pendingBranch = wt.Branch
						m.pendingOpenShell = false
						m.pendingLock = lock
						return m, tea.Quit
					} else if reason != "" {
						m.errMsg = reason
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
			if m.viewPager.Page == prsPage {
				repo := strings.TrimSpace(m.status.RepoRoot)
				needPRList := repo != "" && repo != m.prListLoadedRepo && repo != m.prListFetchingRepo
				if needPRList {
					m.prListFetchingRepo = repo
					return m, tea.Batch(
						pagerCmd,
						fetchGHDataCmd(m.orchestrator, m.status, m.ghFetchingKey, false, false, true, false),
						m.ghSpinner.Tick,
					)
				}
				return m, pagerCmd
			}
			m.prSearchActive = false
			m.prSearchInput.Blur()
			return m, pagerCmd
		}
		if m.viewPager.Page == prsPage {
			if msg.String() == "/" {
				m.prSearchActive = true
				m.prSearchInput.Focus()
				return m, nil
			}
			if m.prSearchActive {
				if msg.Type == tea.KeyEsc {
					if strings.TrimSpace(m.prSearchInput.Value()) != "" {
						m.prSearchInput.SetValue("")
						m.prIndex = 0
						return m, nil
					}
					m.prSearchActive = false
					m.prSearchInput.Blur()
					return m, nil
				}
				if shouldHandlePRSearchInput(msg) {
					var cmd tea.Cmd
					m.prSearchInput, cmd = m.prSearchInput.Update(msg)
					m.prIndex = clampPRIndex(m.prIndex, filteredPRs(m.prs, m.prSearchInput.Value()))
					return m, cmd
				}
			}
		}
		switch msg.String() {
		case "q", "ctrl+c":
			return m, tea.Quit
		case "r":
			if m.viewPager.Page == prsPage {
				repo := strings.TrimSpace(m.status.RepoRoot)
				m.prListLoadedRepo = ""
				m.prListFetchingRepo = ""
				m.prListEnrichingRepo = ""
				m.forcePREnrichRefresh = true
				m.ghWarnMsg = ""
				if repo == "" {
					return m, fetchStatusCmd(m.orchestrator)
				}
				m.prListFetchingRepo = repo
				return m, tea.Batch(
					fetchGHDataCmd(m.orchestrator, m.status, m.ghFetchingKey, true, false, true, false),
					m.ghSpinner.Tick,
				)
			}
			// Force refresh on demand, including GH enrichment on next status update.
			m.ghLoadedKey = ""
			m.ghFetchingKey = ""
			m.prListLoadedRepo = ""
			m.prListFetchingRepo = ""
			m.prListEnrichingRepo = ""
			m.forcePREnrichRefresh = false
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
				maxIndex := len(filteredPRs(m.prs, m.prSearchInput.Value())) - 1
				if m.prIndex < maxIndex {
					m.prIndex++
				}
			}
			return m, nil
		case "enter":
			if m.viewPager.Page == prsPage {
				pr, ok := selectedPR(filteredPRs(m.prs, m.prSearchInput.Value()), m.prIndex)
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
			if m.viewPager.Page != worktreePage {
				return m, nil
			}
			if isCreateRow(m.listIndex, m.status) {
				m.mode = modeAction
				m.actionCreate = true
				m.actionBranch = ""
				m.actionIndex = 0
				m.errMsg = ""
				return m, nil
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
				m.errMsg = ""
				return m, nil
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
				if err := m.mgr.CanDeleteWorktree(row.Path); err != nil {
					m.errMsg = err.Error()
					return m, nil
				}
				m.mode = modeDelete
				m.deletePath = row.Path
				m.deleteBranch = row.Branch
				m.errMsg = ""
				return m, nil
			}
		case "p", "P":
			if m.viewPager.Page == prsPage {
				pr, ok := selectedPR(filteredPRs(m.prs, m.prSearchInput.Value()), m.prIndex)
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
					m.errMsg = "Worktree is not in use."
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
		initialPRLoading := strings.TrimSpace(m.prListFetchingRepo) != ""
		enrichmentLoading := strings.TrimSpace(m.prListEnrichingRepo) != "" && !initialPRLoading
		filtered := filteredPRs(m.prs, m.prSearchInput.Value())
		if m.prSearchActive {
			b.WriteString("Filter PRs (/):\n")
			b.WriteString(inputStyle.Render(m.prSearchInput.View()))
			b.WriteString("\n")
		}
		b.WriteString(baseStyle.Render(renderPRSelector(filtered, m.prIndex, initialPRLoading, m.ghSpinner.View(), enrichmentLoading)))
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
		if pr, ok := selectedPR(filteredPRs(m.prs, m.prSearchInput.Value()), m.prIndex); ok && strings.TrimSpace(pr.URL) != "" {
			b.WriteString("\n")
			b.WriteString(secondaryStyle.Render(pr.URL))
			b.WriteString("\n")
		}
	}
	b.WriteString("\n")
	help := "Press ←/→ to switch views, r to refresh, q to quit."
	if m.viewPager.Page == prsPage {
		help = "Press / to search, up/down to select, enter or P to open URL, esc clears/exits search, ←/→ switch views, r refreshes, q quits."
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

func renderPRSelector(prs []PRListData, cursor int, loading bool, loadingGlyph string, cellLoading bool) string {
	rows := make([]uiview.PRRow, 0, len(prs))
	for _, pr := range prs {
		row := uiview.BuildPRRow(
			pr.Number,
			pr.Branch,
			pr.Title,
			pr.CICompleted,
			pr.CITotal,
			string(pr.CIState),
			pr.CIFailingNames,
			pr.ReviewApproved,
			pr.ReviewRequired,
			pr.ReviewKnown,
			pr.ResolvedComments,
			pr.CommentThreadsTotal,
			pr.Status,
		)
		if cellLoading {
			row.CILabel = loadingGlyph
			row.ApprovalLabel = loadingGlyph
		}
		rows = append(rows, row)
	}
	return uiview.RenderPRSelector(rows, cursor, loading, loadingGlyph, viewStyles())
}

func selectedPR(prs []PRListData, index int) (PRListData, bool) {
	if index < 0 || index >= len(prs) {
		return PRListData{}, false
	}
	return prs[index], true
}

func shouldHandlePRSearchInput(msg tea.KeyMsg) bool {
	switch msg.Type {
	case tea.KeyRunes, tea.KeySpace, tea.KeyBackspace, tea.KeyDelete:
		return true
	default:
		return false
	}
}

func filteredPRs(prs []PRListData, query string) []PRListData {
	q := strings.TrimSpace(strings.ToLower(query))
	if q == "" {
		return prs
	}
	tokens := strings.Fields(q)
	out := make([]PRListData, 0, len(prs))
	for _, pr := range prs {
		haystack := prSearchHaystack(pr)
		matched := true
		for _, token := range tokens {
			if !fuzzyContains(haystack, token) {
				matched = false
				break
			}
		}
		if matched {
			out = append(out, pr)
		}
	}
	return out
}

func prSearchHaystack(pr PRListData) string {
	return strings.ToLower(strings.Join([]string{
		fmt.Sprintf("%d", pr.Number),
		fmt.Sprintf("#%d", pr.Number),
		pr.Branch,
		pr.Title,
		pr.Status,
		pr.ReviewDecision,
		string(pr.CIState),
		fmt.Sprintf("%d/%d", pr.CICompleted, pr.CITotal),
		pr.CIFailingNames,
		fmt.Sprintf("%d", pr.ResolvedComments),
		fmt.Sprintf("%d", pr.CommentThreadsTotal),
		pr.URL,
	}, " "))
}

func fuzzyContains(haystack string, needle string) bool {
	if needle == "" {
		return true
	}
	if strings.Contains(haystack, needle) {
		return true
	}
	h := []rune(haystack)
	n := []rune(needle)
	j := 0
	for i := 0; i < len(h) && j < len(n); i++ {
		if h[i] == n[j] {
			j++
		}
	}
	return j == len(n)
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
	repoRoot              string
	key                   string
	byBranch              map[string]PRData
	prs                   []PRListData
	fetchedByBranch       bool
	fetchedPRList         bool
	fetchedPRListEnriched bool
	err                   error
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

func fetchGHDataCmd(orchestrator *WorktreeOrchestrator, status WorktreeStatus, key string, force bool, includeByBranch bool, includePRList bool, includePRListEnriched bool) tea.Cmd {
	return func() tea.Msg {
		var byBranch map[string]PRData
		var prs []PRListData
		var byBranchErr error
		var prListErr error
		if orchestrator == nil {
			if includeByBranch {
				byBranch = map[string]PRData{}
			}
			if includePRList {
				prs = []PRListData{}
			}
			if includePRListEnriched {
				prs = []PRListData{}
			}
		} else {
			if includeByBranch {
				byBranch, byBranchErr = orchestrator.PRDataForStatusWithError(status, force)
				if byBranch == nil {
					byBranch = map[string]PRData{}
				}
			}
			if includePRList {
				prs, prListErr = orchestrator.PRsForStatusWithError(status, force, false)
				if prs == nil {
					prs = []PRListData{}
				}
			}
			if includePRListEnriched {
				prs, prListErr = orchestrator.PRsForStatusWithError(status, force, true)
				if prs == nil {
					prs = []PRListData{}
				}
			}
		}
		err := byBranchErr
		if err == nil {
			err = prListErr
		}
		return ghDataMsg{
			repoRoot:              status.RepoRoot,
			key:                   key,
			byBranch:              byBranch,
			prs:                   prs,
			fetchedByBranch:       includeByBranch,
			fetchedPRList:         includePRList,
			fetchedPRListEnriched: includePRListEnriched,
			err:                   err,
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

func renderSelector(status WorktreeStatus, cursor int, pendingByBranch map[string]bool, loadingGlyph string) string {
	if !status.InRepo {
		return ""
	}
	rows := make([]uiview.WorktreeRow, 0, len(status.Worktrees)+1)
	orphaned := make(map[string]bool, len(status.Orphaned))
	for _, wt := range status.Orphaned {
		orphaned[wt.Path] = true
	}
	worktrees := worktreesForDisplay(status)
	for _, wt := range worktrees {
		label := wt.Branch
		disabled := false
		if orphaned[wt.Path] {
			label = fmt.Sprintf("%s (orphaned)", wt.Branch)
			disabled = true
		} else if !wt.Available {
			label = wt.Branch + " (in use)"
			disabled = true
		}
		pending := pendingByBranch[strings.TrimSpace(wt.Branch)]
		rows = append(rows, uiview.WorktreeRow{
			BranchLabel:   label,
			PRLabel:       formatPRLabel(wt, pending, loadingGlyph),
			CILabel:       formatCILabel(wt, pending, loadingGlyph),
			ReviewLabel:   formatReviewLabel(wt, pending, loadingGlyph),
			CommentsLabel: formatCommentsLabel(wt, pending, loadingGlyph),
			PRStatusLabel: formatPRStatusLabel(wt, pending, loadingGlyph),
			Disabled:      disabled,
		})
	}
	rows = append(rows, uiview.WorktreeRow{BranchLabel: "+ New worktree"})
	return uiview.RenderWorktreeSelector(rows, cursor, viewStyles())
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

func viewStyles() uiview.Styles {
	return uiview.Styles{
		Header:           func(s string) string { return selectorHeaderStyle.Render(s) },
		Normal:           func(s string) string { return selectorNormalStyle.Render(s) },
		Selected:         func(s string) string { return selectorSelectedStyle.Render(s) },
		Disabled:         func(s string) string { return selectorDisabledStyle.Render(s) },
		DisabledSelected: func(s string) string { return selectorDisabledSelectedStyle.Render(s) },
		Secondary:        func(s string) string { return secondaryStyle.Render(s) },
	}
}

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

func newPRSearchInput() textinput.Model {
	ti := textinput.New()
	ti.Placeholder = "fuzzy search by number, branch, title, status..."
	ti.CharLimit = 200
	ti.Width = 60
	ti.Focus()
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
		if names := strings.TrimSpace(wt.CIFailingNames); names != "" {
			return fmt.Sprintf("✗ %d/%d %s", wt.CIDone, wt.CITotal, names)
		}
		return fmt.Sprintf("✗ %d/%d", wt.CIDone, wt.CITotal)
	case PRCIInProgress:
		return fmt.Sprintf("… %d/%d", wt.CIDone, wt.CITotal)
	default:
		return "-"
	}
}

func formatCommentsLabel(wt WorktreeInfo, pending bool, loadingGlyph string) string {
	if pending {
		return loadingGlyph
	}
	if !wt.HasPR || wt.CommentThreadsTotal <= 0 {
		return "-"
	}
	resolved := wt.ResolvedComments
	if resolved < 0 {
		resolved = 0
	}
	if resolved > wt.CommentThreadsTotal {
		resolved = wt.CommentThreadsTotal
	}
	return fmt.Sprintf("(%d/%d)", resolved, wt.CommentThreadsTotal)
}

func formatReviewLabel(wt WorktreeInfo, pending bool, loadingGlyph string) string {
	if pending {
		return loadingGlyph
	}
	if !wt.HasPR {
		return "-"
	}
	if wt.ReviewRequired > 0 {
		return fmt.Sprintf("%d/%d", wt.ReviewApproved, wt.ReviewRequired)
	}
	if wt.ReviewKnown && wt.ReviewApproved > 0 {
		return "1/1"
	}
	return "-"
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

const maxBranchSuggestions = 15

func availableBranchOptions(status WorktreeStatus, mgr *WorktreeManager, includeInUse bool) ([]string, error) {
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
		if !includeInUse && inUse[opt] {
			continue
		}
		filtered = append(filtered, opt)
	}
	if len(filtered) > maxBranchSuggestions {
		filtered = filtered[:maxBranchSuggestions]
	}
	if len(filtered) == 0 {
		if includeInUse {
			return nil, fmt.Errorf("no local branches found")
		}
		return nil, fmt.Errorf("no available branches (all branches currently in use)")
	}
	return filtered, nil
}

func reusableWorktreeForBranch(status WorktreeStatus, branch string) (WorktreeInfo, bool, string) {
	branch = strings.TrimSpace(branch)
	if branch == "" || !status.InRepo {
		return WorktreeInfo{}, false, ""
	}
	orphaned := make(map[string]bool, len(status.Orphaned))
	for _, wt := range status.Orphaned {
		orphaned[wt.Path] = true
	}
	foundUnavailable := false
	for _, wt := range worktreesForDisplay(status) {
		if strings.TrimSpace(wt.Branch) != branch {
			continue
		}
		if orphaned[wt.Path] {
			return WorktreeInfo{}, false, "Branch has an orphaned worktree. Remove it before reuse."
		}
		if wt.Available {
			return wt, true, ""
		}
		foundUnavailable = true
	}
	if foundUnavailable {
		return WorktreeInfo{}, false, "Branch already has a worktree in use."
	}
	return WorktreeInfo{}, false, ""
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
		status.Worktrees[i].CIFailingNames = ""
		status.Worktrees[i].Approved = false
		status.Worktrees[i].ReviewApproved = 0
		status.Worktrees[i].ReviewRequired = 0
		status.Worktrees[i].ReviewKnown = false
		status.Worktrees[i].UnresolvedComments = 0
		status.Worktrees[i].ResolvedComments = 0
		status.Worktrees[i].CommentThreadsTotal = 0
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
			status.Worktrees[i].CIFailingNames = pr.CIFailingNames
			status.Worktrees[i].Approved = pr.Approved
			status.Worktrees[i].ReviewApproved = pr.ReviewApproved
			status.Worktrees[i].ReviewRequired = pr.ReviewRequired
			status.Worktrees[i].ReviewKnown = pr.ReviewKnown
			status.Worktrees[i].UnresolvedComments = pr.UnresolvedComments
			status.Worktrees[i].ResolvedComments = pr.ResolvedComments
			status.Worktrees[i].CommentThreadsTotal = pr.CommentThreadsTotal
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
