package main

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/lipgloss"
	uiview "github.com/mrbonezy/wtx/ui"
	"github.com/muesli/termenv"
)

type model struct {
	mgr                   *WorktreeManager
	orchestrator          *WorktreeOrchestrator
	runner                *Runner
	status                WorktreeStatus
	listIndex             int
	ready                 bool
	width                 int
	height                int
	mode                  uiMode
	branchInput           textinput.Model
	newBranchInput        textinput.Model
	spinner               spinner.Model
	ghSpinner             spinner.Model
	ghPendingByBranch     map[string]bool
	ghDataByBranch        map[string]PRData
	ghLoadedKey           string
	ghFetchingKey         string
	forceGHRefresh        bool
	ghWarnMsg             string
	updateHint            string
	errMsg                string
	warnMsg               string
	creatingBranch        string
	creatingBaseRef       string
	creatingExisting      bool
	creatingStartedAt     time.Time
	deletePath            string
	deleteBranch          string
	unlockPath            string
	unlockBranch          string
	actionBranch          string
	actionIndex           int
	actionCreate          bool
	branchOptions         []string
	branchSuggestions     []string
	branchIndex           int
	pendingPath           string
	pendingBranch         string
	pendingOpenShell      bool
	pendingLock           *WorktreeLock
	autoActionPath        string
	openLoading           bool
	openLoadErr           string
	openSelected          int
	openTypeahead         string
	openTypeaheadAt       time.Time
	openBranches          []openBranchOption
	openLockedBranches    []openBranchOption
	openSlots             []openSlotState
	openPRBranches        []string
	openFetchID           string
	openShowDebug         bool
	openDebugIndex        int
	openDebugCreating     bool
	openStage             openStage
	openTargetBranch      string
	openTargetIsNew       bool
	openTargetBaseRef     string
	openTargetFetch       bool
	openPickIndex         int
	openPickConfirmPath   string
	openPickConfirmBranch string
	openDefaultBaseRef    string
	openDefaultFetch      bool
	openNewBranchForm     *huh.Form
	openFormBranchPtr     *string
	openFormBaseRefPtr    *string
	openFormFetchPtr      *bool
	confirmForm           *huh.Form
	confirmResult         bool
	confirmKind           confirmKind
	openCreating          bool
	openCreatingStartedAt time.Time
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
	m.mode = modeOpen
	m.openStage = openStageMain
	m.openSelected = 0
	m.openDefaultFetch = true
	if cfg, err := LoadConfig(); err == nil {
		if strings.TrimSpace(cfg.NewBranchBaseRef) != "" {
			m.openDefaultBaseRef = strings.TrimSpace(cfg.NewBranchBaseRef)
		}
		if cfg.NewBranchFetchFirst != nil {
			m.openDefaultFetch = *cfg.NewBranchFetchFirst
		}
	}
	return m
}

func (m model) Init() tea.Cmd {
	return tea.Batch(
		loadOpenScreenCmd(m.orchestrator, m.mgr),
		m.ghSpinner.Tick,
		pollGHTickCmd(),
		pollStatusTickCmd(),
		checkInteractiveUpdateHintCmd(),
	)
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	defer func() {
		syncTabTitleWithSelection(m)
	}()
	if m.confirmForm != nil {
		form, cmd := m.confirmForm.Update(msg)
		if f, ok := form.(*huh.Form); ok {
			m.confirmForm = f
		}
		if m.confirmForm.State == huh.StateCompleted || m.confirmForm.State == huh.StateAborted {
			m.confirmResult = m.confirmForm.State == huh.StateCompleted && m.confirmForm.GetBool(confirmFieldKey)
			return m.handleConfirmDone()
		}
		return m, cmd
	}
	if m.openNewBranchForm != nil {
		applyFormMsg := func(formMsg tea.Msg) (tea.Model, tea.Cmd) {
			form, cmd := m.openNewBranchForm.Update(formMsg)
			if f, ok := form.(*huh.Form); ok {
				m.openNewBranchForm = f
			}
			if m.openNewBranchForm.State == huh.StateCompleted || m.openNewBranchForm.State == huh.StateAborted {
				return m.handleOpenNewBranchFormDone()
			}
			return m, cmd
		}
		if keyMsg, ok := msg.(tea.KeyMsg); ok {
			switch keyMsg.Type {
			case tea.KeyEsc:
				m.openNewBranchForm = nil
				m.openStage = openStageMain
				m.openFormBranchPtr = nil
				m.openFormBaseRefPtr = nil
				m.openFormFetchPtr = nil
				m.errMsg = ""
				return m, nil
			case tea.KeyUp:
				return applyFormMsg(tea.KeyMsg{Type: tea.KeyShiftTab})
			case tea.KeyDown, tea.KeyTab:
				return applyFormMsg(tea.KeyMsg{Type: tea.KeyTab})
			case tea.KeyShiftTab:
				return applyFormMsg(tea.KeyMsg{Type: tea.KeyShiftTab})
			case tea.KeyEnter, tea.KeyCtrlJ:
				return m.submitOpenNewBranchForm()
			case tea.KeyRunes:
				if len(keyMsg.Runes) > 0 && (keyMsg.Runes[0] == '\n' || keyMsg.Runes[0] == '\r') {
					return m.submitOpenNewBranchForm()
				}
			}
			switch keyMsg.String() {
			case "q", "ctrl+c":
				return m, tea.Quit
			case "ctrl+r":
				m.openLoading = true
				m.openLoadErr = ""
				return m, tea.Batch(loadOpenScreenCmd(m.orchestrator, m.mgr), m.ghSpinner.Tick)
			case "esc":
				m.openNewBranchForm = nil
				m.openStage = openStageMain
				m.openFormBranchPtr = nil
				m.openFormBaseRefPtr = nil
				m.openFormFetchPtr = nil
				m.errMsg = ""
				return m, nil
			case "up", "shift+tab":
				return applyFormMsg(tea.KeyMsg{Type: tea.KeyShiftTab})
			case "down", "tab":
				return applyFormMsg(tea.KeyMsg{Type: tea.KeyTab})
			case "enter", "ctrl+m":
				return m.submitOpenNewBranchForm()
			}
		}
		return applyFormMsg(msg)
	}
	switch msg := msg.(type) {
	case interactiveUpdateHintMsg:
		m.updateHint = strings.TrimSpace(msg.hint)
		return m, nil
	case openScreenLoadedMsg:
		m.ready = true
		m.status = msg.status
		m.openLoadErr = ""
		m.errMsg = ""
		if msg.err != nil {
			m.openLoading = false
			m.openBranches = nil
			m.openSlots = nil
			m.openPRBranches = nil
			m.openLoadErr = msg.err.Error()
			return m, nil
		}
		m.openBranches = msg.branches
		m.openLockedBranches = msg.lockedBranches
		m.openSlots = msg.slots
		m.openPRBranches = msg.prBranches
		m.openTypeahead = ""
		m.openDebugIndex = clampOpenDebugIndex(m.openDebugIndex, len(msg.slots))
		m.openDebugCreating = false
		if strings.TrimSpace(m.openDefaultBaseRef) == "" {
			m.openDefaultBaseRef = strings.TrimSpace(msg.status.BaseRef)
			if m.openDefaultBaseRef == "" {
				m.openDefaultBaseRef = "origin/main"
			}
		}
		if m.openStage == openStageMain {
			m.newBranchInput.Blur()
		}
		m.openSelected = clampOpenSelection(m.openSelected, len(m.openBranches))
		m.openFetchID = msg.fetchID
		m.openLoading = true
		var paths []string
		for _, slot := range msg.slots {
			if slot.Path != "" {
				paths = append(paths, slot.Path)
			}
		}
		cmds := []tea.Cmd{m.ghSpinner.Tick}
		if len(paths) > 0 {
			cmds = append(cmds, fetchDirtyStatusCmd(paths))
		}
		if len(m.openPRBranches) == 0 {
			m.openLoading = false
			return m, tea.Batch(cmds...)
		}
		cmds = append(cmds, fetchOpenPRDataCmd(m.orchestrator, m.status.RepoRoot, m.openPRBranches, msg.fetchID))
		return m, tea.Batch(cmds...)
	case openScreenPRDataMsg:
		if strings.TrimSpace(msg.fetchID) == "" || msg.fetchID != m.openFetchID {
			return m, nil
		}
		m.openLoading = false
		if msg.err != nil {
			m.openLoadErr = msg.err.Error()
			return m, nil
		}
		m.openLoadErr = ""
		applyPRDataToOpenState(&m.openBranches, &m.openLockedBranches, &m.openSlots, msg.byBranch)
		return m, nil
	case openScreenDirtyMsg:
		for i := range m.openSlots {
			if dirty, ok := msg.dirtyByPath[m.openSlots[i].Path]; ok {
				m.openSlots[i].Dirty = dirty
			}
		}
		return m, nil
	case openDeleteWorktreeDoneMsg:
		if msg.err != nil {
			m.errMsg = msg.err.Error()
			return m, nil
		}
		m.errMsg = ""
		m.openLoading = true
		return m, tea.Batch(loadOpenScreenCmd(m.orchestrator, m.mgr), m.ghSpinner.Tick)
	case openUnlockWorktreeDoneMsg:
		if msg.err != nil {
			m.errMsg = msg.err.Error()
			return m, nil
		}
		m.errMsg = ""
		m.openLoading = true
		return m, tea.Batch(loadOpenScreenCmd(m.orchestrator, m.mgr), m.ghSpinner.Tick)
	case openCreateWorktreeDoneMsg:
		if msg.err != nil {
			m.errMsg = msg.err.Error()
			return m, nil
		}
		m.errMsg = ""
		m.openLoading = true
		m.openDebugCreating = false
		m.newBranchInput.Blur()
		m.newBranchInput.SetValue("")
		return m, tea.Batch(loadOpenScreenCmd(m.orchestrator, m.mgr), m.ghSpinner.Tick)
	case openUseReadyMsg:
		m.openCreating = false
		m.openCreatingStartedAt = time.Time{}
		if msg.err != nil {
			m.errMsg = msg.err.Error()
			return m, nil
		}
		m.errMsg = ""
		m.warnMsg = ""
		m.pendingPath = msg.path
		m.pendingBranch = msg.branch
		m.pendingOpenShell = msg.openShell
		m.pendingLock = msg.lock
		return m, tea.Quit
	case openDefaultsSavedMsg:
		if msg.err != nil {
			m.errMsg = msg.err.Error()
		}
		return m, nil
	case statusMsg:
		m.status = WorktreeStatus(msg)
		m.listIndex = clampListIndex(m.listIndex, m.status)
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
			m.ghLoadedKey = ""
			m.ghFetchingKey = ""
			m.ghWarnMsg = ""
			return m, nil
		}
		applyPRDataToStatus(&m.status, m.ghDataByBranch)
		return m, nil
	case pollGHTickMsg:
		if m.mode != modeList && m.mode != modeOpen {
			return m, pollGHTickCmd()
		}
		key := ghDataKeyForStatus(m.status)
		if key == "" || key == m.ghFetchingKey {
			return m, pollGHTickCmd()
		}
		m.ghFetchingKey = key
		m.ghPendingByBranch = pendingBranchesByName(m.status)
		force := m.forceGHRefresh
		m.forceGHRefresh = false
		cmd := fetchGHDataCmd(m.orchestrator, m.status, key, force)
		return m, tea.Batch(cmd, m.ghSpinner.Tick, pollGHTickCmd())
	case ghDataMsg:
		if strings.TrimSpace(msg.repoRoot) == "" || strings.TrimSpace(m.status.RepoRoot) == "" {
			return m, nil
		}
		if msg.repoRoot != m.status.RepoRoot {
			return m, nil
		}
		if !msg.fetchedByBranch {
			return m, nil
		}
		if strings.TrimSpace(msg.key) == "" || msg.key != m.ghFetchingKey {
			// Ignore stale GH responses that raced with newer fetches.
			return m, nil
		}
		m.ghWarnMsg = ghWarningFromErr(msg.err)
		m.ghDataByBranch = msg.byBranch
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
		if m.mode == modeOpen && !m.openCreating && m.openStage == openStageMain && !m.openShowDebug && strings.TrimSpace(m.openTypeahead) == "" {
			return m, tea.Batch(loadOpenScreenCmd(m.orchestrator, m.mgr), pollStatusTickCmd())
		}
		return m, pollStatusTickCmd()
	case openPickRefreshTickMsg:
		if m.mode == modeOpen && m.openStage == openStagePickWorktree {
			return m, tea.Batch(loadOpenScreenCmd(m.orchestrator, m.mgr), openPickRefreshTickCmd(), m.ghSpinner.Tick)
		}
		return m, nil
	case createWorktreeDoneMsg:
		m.mode = modeList
		m.creatingBranch = ""
		m.creatingBaseRef = ""
		m.creatingExisting = false
		m.creatingStartedAt = time.Time{}
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
		if m.mode == modeOpen && m.openLoading {
			var cmd tea.Cmd
			m.ghSpinner, cmd = m.ghSpinner.Update(msg)
			if cmd != nil {
				cmds = append(cmds, cmd)
			}
		}
		if m.mode == modeOpen && m.openCreating {
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
		if m.mode == modeOpen {
			switch msg.String() {
			case "q", "ctrl+c":
				return m, tea.Quit
			case "ctrl+d":
				m.openShowDebug = !m.openShowDebug
				m.openDebugIndex = clampOpenDebugIndex(m.openDebugIndex, len(m.openSlots))
				return m, nil
			}
			if m.openShowDebug {
				if m.openDebugCreating {
					switch msg.String() {
					case "enter":
						branch := strings.TrimSpace(m.newBranchInput.Value())
						if branch == "" {
							m.errMsg = "Branch name required."
							return m, nil
						}
						m.errMsg = ""
						return m, createOpenWorktreeCmd(m.mgr, branch, resolveNewBranchBaseRef(m.openDefaultBaseRef, m.status.BaseRef))
					case "esc":
						m.openDebugCreating = false
						m.newBranchInput.Blur()
						m.newBranchInput.SetValue("")
						m.errMsg = ""
						return m, nil
					}
					var cmd tea.Cmd
					m.newBranchInput, cmd = m.newBranchInput.Update(msg)
					return m, cmd
				}
				switch msg.String() {
				case "esc":
					m.openShowDebug = false
					return m, nil
				case "up", "k":
					m.openDebugIndex = clampOpenDebugIndex(m.openDebugIndex-1, len(m.openSlots))
					return m, nil
				case "down", "j":
					m.openDebugIndex = clampOpenDebugIndex(m.openDebugIndex+1, len(m.openSlots))
					return m, nil
				case "d":
					slot, ok := selectedOpenDebugSlot(m.openSlots, m.openDebugIndex)
					if !ok {
						m.errMsg = "No worktree selected in debug list."
						return m, nil
					}
					if slot.Locked {
						m.errMsg = "Cannot remove a worktree that is in use. Unlock it first."
						return m, nil
					}
					if slot.Dirty {
						m.errMsg = "Cannot remove an unclean worktree."
						return m, nil
					}
					m.openPickConfirmPath = slot.Path
					m.openPickConfirmBranch = slot.Branch
					m.confirmResult = false
					m.confirmKind = confirmOpenDebugDelete
					m.confirmForm = newConfirmForm(
						"Delete selected worktree?",
						fmt.Sprintf("%s\n%s", slot.Branch, slot.Path),
						&m.confirmResult,
					)
					m.errMsg = ""
					return m, m.confirmForm.Init()
				case "u":
					slot, ok := selectedOpenDebugSlot(m.openSlots, m.openDebugIndex)
					if !ok {
						m.errMsg = "No worktree selected in debug list."
						return m, nil
					}
					if !slot.Locked {
						m.errMsg = "Worktree is not in use."
						return m, nil
					}
					m.openPickConfirmPath = slot.Path
					m.openPickConfirmBranch = slot.Branch
					m.confirmResult = false
					m.confirmKind = confirmOpenDebugUnlock
					m.confirmForm = newConfirmForm(
						"Force unlock selected worktree?",
						fmt.Sprintf("%s\n%s", slot.Branch, slot.Path),
						&m.confirmResult,
					)
					m.errMsg = ""
					return m, m.confirmForm.Init()
				case "n":
					m.openDebugCreating = true
					m.newBranchInput.SetValue("")
					m.newBranchInput.Focus()
					m.errMsg = ""
					return m, nil
				case "ctrl+r":
					m.openLoading = true
					m.openLoadErr = ""
					m.openTypeahead = ""
					return m, tea.Batch(loadOpenScreenCmd(m.orchestrator, m.mgr), m.ghSpinner.Tick)
				}
				return m, nil
			}
			if m.openStage == openStageNewBranchConfig {
				switch msg.String() {
				case "esc":
					m.openStage = openStageMain
					m.openNewBranchForm = nil
					m.openFormBranchPtr = nil
					m.openFormBaseRefPtr = nil
					m.openFormFetchPtr = nil
					m.errMsg = ""
				}
				return m, nil
			}
			if m.openStage == openStagePickWorktree {
				switch msg.String() {
				case "ctrl+r":
					m.openLoading = true
					m.openLoadErr = ""
					return m, tea.Batch(loadOpenScreenCmd(m.orchestrator, m.mgr), openPickRefreshTickCmd(), m.ghSpinner.Tick)
				case "esc":
					m.openStage = openStageMain
					return m, nil
				case "up", "k":
					m.openPickIndex = clampOpenPickIndex(m.openPickIndex-1, m.openSlots)
					return m, nil
				case "down", "j":
					m.openPickIndex = clampOpenPickIndex(m.openPickIndex+1, m.openSlots)
					return m, nil
				case "enter":
					if m.openPickIndex == 0 {
						m.openCreating = true
						m.openCreatingStartedAt = time.Now()
						return m, tea.Batch(m.spinner.Tick, openCmdForCreateTarget(m))
					}
					slot, ok := selectedOpenDebugSlot(m.openSlots, m.openPickIndex-1)
					if !ok {
						m.errMsg = "No worktree selected."
						return m, nil
					}
					if slot.Locked {
						m.openPickConfirmPath = slot.Path
						m.openPickConfirmBranch = slot.Branch
						m.confirmResult = false
						m.confirmKind = confirmOpenPickLocked
						m.confirmForm = newConfirmForm(
							"Force unlock selected worktree?",
							fmt.Sprintf("%s\n%s", slot.Branch, slot.Path),
							&m.confirmResult,
						)
						return m, m.confirmForm.Init()
					}
					if slot.Dirty {
						m.warnMsg = "Worktree is unclean. Clean it first."
						m.pendingPath = slot.Path
						m.pendingBranch = slot.Branch
						m.pendingOpenShell = true
						m.pendingLock = nil
						return m, tea.Quit
					}
					m.openCreating = true
					m.openCreatingStartedAt = time.Now()
					return m, tea.Batch(m.spinner.Tick, openCmdForTargetOnSlot(m, slot))
				}
				return m, nil
			}
			if msg.String() == "ctrl+r" {
				m.openLoading = true
				m.openLoadErr = ""
				m.openTypeahead = ""
				return m, tea.Batch(loadOpenScreenCmd(m.orchestrator, m.mgr), m.ghSpinner.Tick)
			}
			switch msg.String() {
			case "up":
				filtered := openFilteredIndices(m.openTypeahead, m.openBranches)
				m.openSelected = moveOpenSelection(m.openSelected, -1, filtered)
				return m, nil
			case "down":
				filtered := openFilteredIndices(m.openTypeahead, m.openBranches)
				m.openSelected = moveOpenSelection(m.openSelected, 1, filtered)
				return m, nil
			case "enter":
				if m.openSelected == 0 {
					defaultBase := resolveNewBranchBaseRef(m.openDefaultBaseRef, m.status.BaseRef)
					branch := ""
					baseRef := defaultBase
					fetch := m.openDefaultFetch
					m.openStage = openStageNewBranchConfig
					m.openFormBranchPtr = &branch
					m.openFormBaseRefPtr = &baseRef
					m.openFormFetchPtr = &fetch
					m.openNewBranchForm = newOpenNewBranchForm(m.openFormBranchPtr, m.openFormBaseRefPtr, m.openFormFetchPtr)
					m.openTypeahead = ""
					m.errMsg = ""
					return m, m.openNewBranchForm.Init()
				}
				index := m.openSelected - 1
				if index < 0 || index >= len(m.openBranches) {
					m.errMsg = "No branch selected."
					return m, nil
				}
				branch := strings.TrimSpace(m.openBranches[index].Name)
				if branch == "" {
					m.errMsg = "No branch selected."
					return m, nil
				}
				m.openTargetBranch = branch
				m.openTargetIsNew = false
				m.openTargetBaseRef = ""
				m.openTargetFetch = false
				m.errMsg = ""
				return m.continueOpenTargetSelection(nil)
			case "esc":
				return m, nil
			}
			switch msg.Type {
			case tea.KeyRunes:
				queryPart := string(msg.Runes)
				if strings.TrimSpace(queryPart) == "" {
					return m, nil
				}
				now := time.Now()
				if m.openTypeahead == "" || now.Sub(m.openTypeaheadAt) > 2*time.Second {
					m.openTypeahead = queryPart
				} else {
					m.openTypeahead += queryPart
				}
				m.openTypeaheadAt = now
				filtered := openFilteredIndices(m.openTypeahead, m.openBranches)
				m.openSelected = ensureOpenSelectionVisible(m.openSelected, filtered)
				if m.openSelected == 0 && len(filtered) > 0 {
					m.openSelected = filtered[0] + 1
				}
				m.errMsg = ""
				return m, nil
			case tea.KeyBackspace, tea.KeyDelete:
				if m.openTypeahead == "" {
					return m, nil
				}
				r := []rune(m.openTypeahead)
				if len(r) <= 1 {
					m.openTypeahead = ""
				} else {
					m.openTypeahead = string(r[:len(r)-1])
				}
				m.openTypeaheadAt = time.Now()
				filtered := openFilteredIndices(m.openTypeahead, m.openBranches)
				m.openSelected = ensureOpenSelectionVisible(m.openSelected, filtered)
				if strings.TrimSpace(m.openTypeahead) != "" && m.openSelected == 0 && len(filtered) > 0 {
					m.openSelected = filtered[0] + 1
				}
				m.errMsg = ""
				return m, nil
			}
			return m, nil
		}
		if m.mode == modeCreating {
			switch msg.String() {
			case "q", "ctrl+c":
				return m, tea.Quit
			}
			return m, nil
		}
		if m.mode == modeDelete || m.mode == modeUnlock {
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
				if !m.actionCreate {
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
					if err := m.mgr.CheckoutNewBranch(row.Path, branch, resolveNewBranchBaseRef(m.openDefaultBaseRef, m.status.BaseRef), m.openDefaultFetch); err != nil {
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
				m.mode = modeCreating
				m.creatingBranch = branch
				m.creatingBaseRef = resolveNewBranchBaseRef(m.openDefaultBaseRef, m.status.BaseRef)
				m.creatingExisting = false
				m.creatingStartedAt = time.Now()
				m.newBranchInput.Blur()
				m.newBranchInput.SetValue("")
				m.errMsg = ""
				return m, tea.Batch(
					m.spinner.Tick,
					createWorktreeCmd(m.mgr, branch, resolveNewBranchBaseRef(m.openDefaultBaseRef, m.status.BaseRef)),
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
				if m.actionIndex < len(currentActionItems(m.actionBranch, resolveNewBranchBaseRef(m.openDefaultBaseRef, m.status.BaseRef), m.actionCreate))-1 {
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
					m.creatingBaseRef = ""
					m.creatingExisting = true
					m.creatingStartedAt = time.Now()
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
			if m.listIndex > 0 {
				m.listIndex--
			}
			return m, nil
		case "down", "j":
			maxIndex := selectorRowCount(m.status) - 1
			if m.listIndex < maxIndex {
				m.listIndex++
			}
			return m, nil
		case "enter":
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
			if row, ok := selectedWorktree(m.status, m.listIndex); ok {
				if err := m.mgr.CanDeleteWorktree(row.Path); err != nil {
					m.errMsg = err.Error()
					return m, nil
				}
				m.mode = modeDelete
				m.deletePath = row.Path
				m.deleteBranch = row.Branch
				m.confirmResult = false
				m.confirmKind = confirmDelete
				m.confirmForm = newConfirmForm(
					"Delete worktree?",
					fmt.Sprintf("%s\n%s", row.Branch, row.Path),
					&m.confirmResult,
				)
				m.errMsg = ""
				return m, m.confirmForm.Init()
			}
		case "p", "P":
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
				m.confirmResult = false
				m.confirmKind = confirmUnlock
				m.confirmForm = newConfirmForm(
					"Unlock worktree?",
					fmt.Sprintf("%s\n%s", row.Branch, row.Path),
					&m.confirmResult,
				)
				m.errMsg = ""
				return m, m.confirmForm.Init()
			}
		}
	}
	return m, nil
}

func (m model) handleConfirmDone() (tea.Model, tea.Cmd) {
	kind := m.confirmKind
	confirmed := m.confirmResult
	m.confirmForm = nil
	m.confirmResult = false
	m.confirmKind = confirmNone

	switch kind {
	case confirmDelete:
		m.mode = modeList
		path := m.deletePath
		m.deletePath = ""
		m.deleteBranch = ""
		m.errMsg = ""
		if !confirmed {
			return m, nil
		}
		force := isOrphanedPath(m.status, path)
		if err := m.mgr.DeleteWorktree(path, force); err != nil {
			m.errMsg = err.Error()
			return m, nil
		}
		return m, fetchStatusCmd(m.orchestrator)
	case confirmUnlock:
		m.mode = modeList
		path := m.unlockPath
		m.unlockPath = ""
		m.unlockBranch = ""
		m.errMsg = ""
		if !confirmed {
			return m, nil
		}
		if err := m.mgr.UnlockWorktree(path); err != nil {
			m.errMsg = err.Error()
			return m, nil
		}
		return m, fetchStatusCmd(m.orchestrator)
	case confirmOpenDebugDelete:
		path := m.openPickConfirmPath
		m.openPickConfirmPath = ""
		m.openPickConfirmBranch = ""
		if !confirmed {
			return m, nil
		}
		return m, deleteOpenWorktreeCmd(m.mgr, path)
	case confirmOpenDebugUnlock:
		path := m.openPickConfirmPath
		m.openPickConfirmPath = ""
		m.openPickConfirmBranch = ""
		if !confirmed {
			return m, nil
		}
		return m, unlockOpenWorktreeCmd(m.mgr, path)
	case confirmOpenPickLocked:
		path := m.openPickConfirmPath
		m.openPickConfirmPath = ""
		m.openPickConfirmBranch = ""
		if !confirmed {
			return m, nil
		}
		if err := m.mgr.UnlockWorktree(path); err != nil {
			m.errMsg = err.Error()
			return m, nil
		}
		if slot, ok := findOpenSlotByPath(m.openSlots, path); ok && slot.Dirty {
			m.warnMsg = "Worktree is unclean. Clean it first."
			m.pendingPath = slot.Path
			m.pendingBranch = slot.Branch
			m.pendingOpenShell = true
			m.pendingLock = nil
			return m, tea.Quit
		}
		if slot, ok := findOpenSlotByPath(m.openSlots, path); ok {
			m.openCreating = true
			m.openCreatingStartedAt = time.Now()
			return m, tea.Batch(m.spinner.Tick, openCmdForTargetOnSlot(m, slot))
		}
		m.openLoading = true
		return m, tea.Batch(loadOpenScreenCmd(m.orchestrator, m.mgr), openPickRefreshTickCmd(), m.ghSpinner.Tick)
	case confirmOpenBaseDefault:
		var saveCmd tea.Cmd
		if confirmed {
			m.openDefaultBaseRef = strings.TrimSpace(m.openTargetBaseRef)
			saveCmd = saveOpenDefaultsCmd(m.openDefaultBaseRef, m.openDefaultFetch)
		}
		if m.openTargetFetch != m.openDefaultFetch {
			m.confirmResult = false
			m.confirmKind = confirmOpenFetchDefault
			m.confirmForm = newConfirmForm(
				"Save this fetch preference as default?",
				fmt.Sprintf("%s\ngit fetch first: %t", m.openTargetBranch, m.openTargetFetch),
				&m.confirmResult,
			)
			if saveCmd != nil {
				return m, tea.Batch(saveCmd, m.confirmForm.Init())
			}
			return m, m.confirmForm.Init()
		}
		return m.continueOpenTargetSelection(saveCmd)
	case confirmOpenFetchDefault:
		var saveCmd tea.Cmd
		if confirmed {
			m.openDefaultFetch = m.openTargetFetch
			saveCmd = saveOpenDefaultsCmd(m.openDefaultBaseRef, m.openDefaultFetch)
		}
		return m.continueOpenTargetSelection(saveCmd)
	default:
		return m, nil
	}
}

func (m model) handleOpenNewBranchFormDone() (tea.Model, tea.Cmd) {
	if m.openNewBranchForm == nil {
		return m, nil
	}
	aborted := m.openNewBranchForm.State == huh.StateAborted
	if aborted {
		m.openNewBranchForm = nil
		m.openStage = openStageMain
		m.openFormBranchPtr = nil
		m.openFormBaseRefPtr = nil
		m.openFormFetchPtr = nil
		m.errMsg = ""
		return m, nil
	}
	return m.submitOpenNewBranchForm()
}

func (m model) submitOpenNewBranchForm() (tea.Model, tea.Cmd) {
	branch := ""
	base := ""
	fetch := m.openDefaultFetch
	if m.openFormBranchPtr != nil {
		branch = strings.TrimSpace(*m.openFormBranchPtr)
	}
	if m.openFormBaseRefPtr != nil {
		base = strings.TrimSpace(*m.openFormBaseRefPtr)
	}
	if m.openFormFetchPtr != nil {
		fetch = *m.openFormFetchPtr
	}
	if m.openNewBranchForm != nil {
		if focused := m.openNewBranchForm.GetFocusedField(); focused != nil {
			switch focused.GetKey() {
			case openNewBranchNameKey:
				if v := strings.TrimSpace(fmt.Sprint(focused.GetValue())); v != "" {
					branch = v
				}
			case openNewBaseRefKey:
				if v := strings.TrimSpace(fmt.Sprint(focused.GetValue())); v != "" {
					base = v
				}
			case openNewFetchKey:
				if v, ok := focused.GetValue().(bool); ok {
					fetch = v
				}
			}
		}
	}
	if branch == "" {
		m.errMsg = "Branch name required."
		return m, nil
	}
	if base == "" {
		base = m.openDefaultBaseRef
	}
	if strings.TrimSpace(base) == "" {
		base = "origin/main"
	}
	m.openTargetBranch = branch
	m.openTargetIsNew = true
	m.openTargetBaseRef = base
	m.openTargetFetch = fetch
	m.openNewBranchForm = nil
	m.openFormBranchPtr = nil
	m.openFormBaseRefPtr = nil
	m.openFormFetchPtr = nil
	m.openStage = openStageMain
	m.errMsg = ""
	if m.openTargetBaseRef != m.openDefaultBaseRef {
		m.confirmResult = false
		m.confirmKind = confirmOpenBaseDefault
		m.confirmForm = newConfirmForm(
			"Save this base ref as default?",
			fmt.Sprintf("%s\n%s", m.openTargetBranch, m.openTargetBaseRef),
			&m.confirmResult,
		)
		return m, m.confirmForm.Init()
	}
	if m.openTargetFetch != m.openDefaultFetch {
		m.confirmResult = false
		m.confirmKind = confirmOpenFetchDefault
		m.confirmForm = newConfirmForm(
			"Save this fetch preference as default?",
			fmt.Sprintf("%s\ngit fetch first: %t", m.openTargetBranch, m.openTargetFetch),
			&m.confirmResult,
		)
		return m, m.confirmForm.Init()
	}
	return m.continueOpenTargetSelection(nil)
}

func (m model) continueOpenTargetSelection(saveCmd tea.Cmd) (tea.Model, tea.Cmd) {
	if slot, ok := m.orchestrator.ResolveOpenTargetSlot(m.openSlots, m.openTargetBranch, m.openTargetIsNew); ok {
		m.openCreating = true
		m.openCreatingStartedAt = time.Now()
		cmds := []tea.Cmd{m.spinner.Tick, openCmdForTargetOnSlot(m, slot)}
		if saveCmd != nil {
			cmds = append([]tea.Cmd{saveCmd}, cmds...)
		}
		return m, tea.Batch(cmds...)
	}
	m.openStage = openStagePickWorktree
	m.openPickIndex = 0
	cmds := []tea.Cmd{loadOpenScreenCmd(m.orchestrator, m.mgr), openPickRefreshTickCmd(), m.ghSpinner.Tick}
	if saveCmd != nil {
		cmds = append([]tea.Cmd{saveCmd}, cmds...)
	}
	return m, tea.Batch(cmds...)
}

func syncTabTitleWithSelection(m model) {
	if !m.ready || !m.status.InRepo {
		setITermWTXTab()
		return
	}
	if wt, ok := selectedWorktree(m.status, m.listIndex); ok {
		setITermWTXBranchTab(wt.Branch)
		return
	}
	setITermWTXTab()
}
func (m model) View() string {
	var b strings.Builder
	showTopBar := m.ready && m.status.InRepo && m.mode == modeList
	if showTopBar {
		b.WriteString(renderViewHeader())
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

	if m.confirmForm != nil {
		b.WriteString(m.confirmForm.View())
		return b.String()
	}

	if m.mode == modeOpen {
		b.WriteString(renderOpenScreen(m))
		return b.String()
	}

	if m.mode == modeAction {
		title := "Worktree actions:"
		if m.actionCreate {
			title = "New worktree actions:"
		}
		b.WriteString(title + "\n")
		for i, item := range currentActionItems(m.actionBranch, resolveNewBranchBaseRef(m.openDefaultBaseRef, m.status.BaseRef), m.actionCreate) {
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
	b.WriteString(baseStyle.Render(renderSelector(m.status, m.listIndex, m.ghPendingByBranch, m.ghSpinner.View())))
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
		b.WriteString(" ")
		b.WriteString(renderCreateProgress(m))
		b.WriteString("\n")
	}
	if m.warnMsg != "" {
		b.WriteString(warnStyle.Render(m.warnMsg))
		b.WriteString("\n")
	}
	if m.ghWarnMsg != "" {
		b.WriteString(warnStyle.Render(m.ghWarnMsg))
		b.WriteString("\n")
	}
	if m.updateHint != "" {
		b.WriteString(warnStyle.Render(m.updateHint))
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
	selectedPath := currentWorktreePath(m.status, m.listIndex)
	if selectedPath != "" {
		b.WriteString("\n")
		b.WriteString(secondaryStyle.Render(selectedPath))
		b.WriteString("\n")
	}

	b.WriteString("\n")
	help := "Press r to refresh, q to quit."
	if m.mode == modeCreating {
		help = "Creating worktree..."
	} else if isCreateRow(m.listIndex, m.status) {
		help = "Press enter for actions, r to refresh, q to quit."
	} else if wt, ok := selectedWorktree(m.status, m.listIndex); ok {
		prHint := ""
		if strings.TrimSpace(wt.PRURL) != "" {
			prHint = ", p to open PR"
		}
		if !wt.Available && !isOrphanedPath(m.status, wt.Path) {
			help = "Press u to unlock, d to delete" + prHint + ", r to refresh, q to quit."
		} else {
			help = "Press enter for actions, s for shell, d to delete" + prHint + ", r to refresh, q to quit."
		}
	}
	b.WriteString(help + "\n")
	return b.String()
}
func renderViewHeader() string {
	return lipgloss.NewStyle().Foreground(lipgloss.Color("252")).Render("Worktrees")
}

func renderCreateProgress(m model) string {
	branch := strings.TrimSpace(m.creatingBranch)
	if branch == "" {
		branch = "branch"
	}
	elapsed := ""
	if !m.creatingStartedAt.IsZero() {
		elapsed = fmt.Sprintf(" (%ds)", int(time.Since(m.creatingStartedAt).Seconds()))
	}
	if m.creatingExisting {
		return fmt.Sprintf("Provisioning worktree for %s%s...", branchStyle.Render(branch), elapsed)
	}
	base := strings.TrimSpace(m.creatingBaseRef)
	if base == "" {
		base = resolveNewBranchBaseRef(m.openDefaultBaseRef, m.status.BaseRef)
	}
	if base != "" {
		return fmt.Sprintf("Provisioning %s from %s%s...", branchStyle.Render(branch), branchInlineStyle.Render(base), elapsed)
	}
	return fmt.Sprintf("Provisioning %s%s...", branchStyle.Render(branch), elapsed)
}
func shouldFetchByBranch(key string, loadedKey string, fetchingKey string) bool {
	key = strings.TrimSpace(key)
	if key == "" {
		return false
	}
	return key != strings.TrimSpace(loadedKey) && key != strings.TrimSpace(fetchingKey)
}

type statusMsg WorktreeStatus
type pollStatusTickMsg time.Time
type pollGHTickMsg time.Time
type openPickRefreshTickMsg time.Time
type ghDataMsg struct {
	repoRoot        string
	key             string
	byBranch        map[string]PRData
	fetchedByBranch bool
	err             error
}
type createWorktreeDoneMsg struct {
	created WorktreeInfo
	err     error
}
type openDeleteWorktreeDoneMsg struct {
	path string
	err  error
}
type openUnlockWorktreeDoneMsg struct {
	path string
	err  error
}
type openCreateWorktreeDoneMsg struct {
	created WorktreeInfo
	err     error
}
type openUseReadyMsg struct {
	path      string
	branch    string
	lock      *WorktreeLock
	openShell bool
	err       error
}
type openDefaultsSavedMsg struct {
	err error
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
	return tea.Tick(2*time.Second, func(t time.Time) tea.Msg {
		return pollStatusTickMsg(t)
	})
}

func pollGHTickCmd() tea.Cmd {
	return tea.Tick(5*time.Second, func(t time.Time) tea.Msg {
		return pollGHTickMsg(t)
	})
}

func openPickRefreshTickCmd() tea.Cmd {
	return tea.Tick(2*time.Second, func(t time.Time) tea.Msg {
		return openPickRefreshTickMsg(t)
	})
}

func fetchGHDataCmd(orchestrator *WorktreeOrchestrator, status WorktreeStatus, key string, force bool) tea.Cmd {
	return func() tea.Msg {
		var byBranch map[string]PRData
		var byBranchErr error
		if orchestrator == nil {
			byBranch = map[string]PRData{}
		} else {
			byBranch, byBranchErr = orchestrator.PRDataForStatusWithError(status, force)
			if byBranch == nil {
				byBranch = map[string]PRData{}
			}
		}
		return ghDataMsg{
			repoRoot:        status.RepoRoot,
			key:             key,
			byBranch:        byBranch,
			fetchedByBranch: true,
			err:             byBranchErr,
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

func deleteOpenWorktreeCmd(mgr *WorktreeManager, path string) tea.Cmd {
	return func() tea.Msg {
		if mgr == nil {
			return openDeleteWorktreeDoneMsg{path: path, err: fmt.Errorf("worktree manager unavailable")}
		}
		err := mgr.DeleteWorktree(path, false)
		return openDeleteWorktreeDoneMsg{path: path, err: err}
	}
}

func unlockOpenWorktreeCmd(mgr *WorktreeManager, path string) tea.Cmd {
	return func() tea.Msg {
		if mgr == nil {
			return openUnlockWorktreeDoneMsg{path: path, err: fmt.Errorf("worktree manager unavailable")}
		}
		err := mgr.UnlockWorktree(path)
		return openUnlockWorktreeDoneMsg{path: path, err: err}
	}
}

func createOpenWorktreeCmd(mgr *WorktreeManager, branch string, baseRef string) tea.Cmd {
	return func() tea.Msg {
		if mgr == nil {
			return openCreateWorktreeDoneMsg{err: fmt.Errorf("worktree manager unavailable")}
		}
		created, err := mgr.CreateWorktree(branch, baseRef)
		return openCreateWorktreeDoneMsg{created: created, err: err}
	}
}

func useExistingWorktreeCmd(mgr *WorktreeManager, path string, branch string) tea.Cmd {
	return func() tea.Msg {
		lock, err := mgr.AcquireWorktreeLock(path)
		if err != nil {
			return openUseReadyMsg{err: err}
		}
		return openUseReadyMsg{path: path, branch: branch, lock: lock}
	}
}

func checkoutExistingInWorktreeCmd(mgr *WorktreeManager, path string, branch string) tea.Cmd {
	return func() tea.Msg {
		lock, err := mgr.AcquireWorktreeLock(path)
		if err != nil {
			return openUseReadyMsg{err: err}
		}
		if err := mgr.CheckoutExistingBranch(path, branch); err != nil {
			lock.Release()
			return openUseReadyMsg{err: err}
		}
		return openUseReadyMsg{path: path, branch: branch, lock: lock}
	}
}

func checkoutNewInWorktreeCmd(mgr *WorktreeManager, path string, branch string, baseRef string, doFetch bool) tea.Cmd {
	return func() tea.Msg {
		lock, err := mgr.AcquireWorktreeLock(path)
		if err != nil {
			return openUseReadyMsg{err: err}
		}
		if err := mgr.CheckoutNewBranch(path, branch, baseRef, doFetch); err != nil {
			lock.Release()
			return openUseReadyMsg{err: err}
		}
		return openUseReadyMsg{path: path, branch: branch, lock: lock}
	}
}

func createAndUseExistingWorktreeCmd(mgr *WorktreeManager, branch string) tea.Cmd {
	return func() tea.Msg {
		created, err := mgr.CreateWorktreeFromBranch(branch)
		if err != nil {
			return openUseReadyMsg{err: err}
		}
		lock, err := mgr.AcquireWorktreeLock(created.Path)
		if err != nil {
			return openUseReadyMsg{err: err}
		}
		return openUseReadyMsg{path: created.Path, branch: branch, lock: lock}
	}
}

func createAndUseNewWorktreeCmd(mgr *WorktreeManager, branch string, baseRef string, doFetch bool) tea.Cmd {
	return func() tea.Msg {
		if doFetch {
			if err := mgr.FetchRepo(); err != nil {
				return openUseReadyMsg{err: err}
			}
		}
		created, err := mgr.CreateWorktree(branch, baseRef)
		if err != nil {
			return openUseReadyMsg{err: err}
		}
		lock, err := mgr.AcquireWorktreeLock(created.Path)
		if err != nil {
			return openUseReadyMsg{err: err}
		}
		return openUseReadyMsg{path: created.Path, branch: branch, lock: lock}
	}
}

func saveOpenDefaultsCmd(baseRef string, fetch bool) tea.Cmd {
	return func() tea.Msg {
		cfg, err := LoadConfig()
		if err != nil {
			exists, exErr := ConfigExists()
			if exErr != nil {
				return openDefaultsSavedMsg{err: exErr}
			}
			if exists {
				return openDefaultsSavedMsg{err: err}
			}
			cfg = Config{AgentCommand: defaultAgentCommand}
		}
		baseRef = strings.TrimSpace(baseRef)
		if baseRef != "" {
			cfg.NewBranchBaseRef = baseRef
		}
		v := fetch
		cfg.NewBranchFetchFirst = &v
		if err := SaveConfig(cfg); err != nil {
			return openDefaultsSavedMsg{err: err}
		}
		return openDefaultsSavedMsg{}
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
			BranchLabel:     label,
			PRLabel:         formatPRLabel(wt, pending, loadingGlyph),
			CILabel:         formatCILabel(wt, pending, loadingGlyph),
			ReviewLabel:     formatReviewLabel(wt, pending, loadingGlyph),
			CommentsLabel:   formatCommentsLabel(wt, pending, loadingGlyph),
			UnresolvedLabel: formatUnresolvedLabel(wt, pending, loadingGlyph),
			PRStatusLabel:   formatPRStatusLabel(wt, pending, loadingGlyph),
			Disabled:        disabled,
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
	modeOpen uiMode = iota
	modeList
	modeCreating
	modeDelete
	modeUnlock
	modeAction
	modeBranchName
	modeBranchPick
)

type openStage int

const (
	openStageMain openStage = iota
	openStageNewBranchConfig
	openStagePickWorktree
)

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
	label := fmt.Sprintf("#%d", wt.PRNumber)
	if strings.TrimSpace(wt.PRURL) != "" {
		return termenv.Hyperlink(wt.PRURL, label)
	}
	return label
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
	case "merged", "closed", "conflict", "can-merge", "awaiting-review", "awaiting-ci", "awaiting-comments", "draft", "open":
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
	if !wt.HasPR || !wt.CommentsKnown || wt.CommentThreadsTotal <= 0 {
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

func formatUnresolvedLabel(wt WorktreeInfo, pending bool, loadingGlyph string) string {
	if pending {
		return loadingGlyph
	}
	if !wt.HasPR || !wt.CommentsKnown {
		return "-"
	}
	unresolved := wt.UnresolvedComments
	if unresolved < 0 {
		unresolved = 0
	}
	return fmt.Sprintf("%d", unresolved)
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
		status.Worktrees[i].CommentsKnown = false
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
			status.Worktrees[i].CommentsKnown = pr.CommentsKnown
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

func clampOpenDebugIndex(index int, count int) int {
	if count <= 0 {
		return 0
	}
	if index < 0 {
		return 0
	}
	if index >= count {
		return count - 1
	}
	return index
}

func selectedOpenDebugSlot(slots []openSlotState, index int) (openSlotState, bool) {
	if index < 0 || index >= len(slots) {
		return openSlotState{}, false
	}
	return slots[index], true
}

func openCmdForTargetOnSlot(m model, slot openSlotState) tea.Cmd {
	if m.openTargetIsNew {
		return checkoutNewInWorktreeCmd(m.mgr, slot.Path, m.openTargetBranch, m.openTargetBaseRef, m.openTargetFetch)
	}
	if strings.TrimSpace(slot.Branch) == strings.TrimSpace(m.openTargetBranch) {
		return useExistingWorktreeCmd(m.mgr, slot.Path, m.openTargetBranch)
	}
	return checkoutExistingInWorktreeCmd(m.mgr, slot.Path, m.openTargetBranch)
}

func openCmdForCreateTarget(m model) tea.Cmd {
	if m.openTargetIsNew {
		return createAndUseNewWorktreeCmd(m.mgr, m.openTargetBranch, m.openTargetBaseRef, m.openTargetFetch)
	}
	return createAndUseExistingWorktreeCmd(m.mgr, m.openTargetBranch)
}

func findOpenSlotByPath(slots []openSlotState, path string) (openSlotState, bool) {
	needle := strings.TrimSpace(path)
	if needle == "" {
		return openSlotState{}, false
	}
	for _, slot := range slots {
		if strings.TrimSpace(slot.Path) == needle {
			return slot, true
		}
	}
	return openSlotState{}, false
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
