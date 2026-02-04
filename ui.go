package main

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/table"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type model struct {
	mgr    *WorktreeManager
	status WorktreeStatus
	table  table.Model
	ready  bool
	width  int
	height int
	mode   uiMode
	input  textinput.Model
	branchInput textinput.Model
	errMsg string
	deletePath   string
	deleteBranch string
	actionBranch string
	actionIndex  int
	branchOptions     []string
	branchSuggestions []string
	branchIndex       int
}

func newModel() model {
	mgr := NewWorktreeManager("")
	m := model{mgr: mgr}
	m.table = newTable()
	m.input = newTextInput()
	m.branchInput = newBranchInput()
	return m
}

func (m model) Init() tea.Cmd {
	return tea.Batch(fetchStatusCmd(m.mgr), tea.ExitAltScreen, tea.ClearScreen)
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case statusMsg:
		m.status = WorktreeStatus(msg)
		m.table.SetRows(worktreeRows(m.status))
		m.ready = true
		return m, nil
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil
	case tea.KeyMsg:
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
				return m, fetchStatusCmd(m.mgr)
			case "n", "N", "esc":
				m.mode = modeList
				m.deletePath = ""
				m.deleteBranch = ""
				m.errMsg = ""
				return m, nil
			}
			return m, nil
		}
		if m.mode == modeCreate {
			switch msg.String() {
			case "esc":
				m.mode = modeList
				m.input.Blur()
				m.errMsg = ""
				return m, nil
			case "enter":
				branch := strings.TrimSpace(m.input.Value())
				if branch == "" {
					m.errMsg = "Branch name required."
					return m, nil
				}
				_, err := m.mgr.CreateWorktree(branch)
				if err != nil {
					m.errMsg = err.Error()
					return m, nil
				}
				m.mode = modeList
				m.input.Blur()
				m.input.SetValue("")
				m.errMsg = ""
				return m, fetchStatusCmd(m.mgr)
			}
			var cmd tea.Cmd
			m.input, cmd = m.input.Update(msg)
			return m, cmd
		}
		if m.mode == modeAction {
			switch msg.String() {
			case "esc":
				m.mode = modeList
				m.actionIndex = 0
				m.actionBranch = ""
				return m, nil
			case "up", "k":
				if m.actionIndex > 0 {
					m.actionIndex--
				}
				return m, nil
			case "down", "j":
				if m.actionIndex < len(actionItems(m.actionBranch, m.status.BaseRef))-1 {
					m.actionIndex++
				}
				return m, nil
			case "enter":
				if m.actionIndex == 2 {
					m.mode = modeBranchPick
					m.branchOptions = uniqueBranches(m.status)
					m.branchSuggestions = filterBranches(m.branchOptions, "")
					m.branchIndex = 0
					m.branchInput.SetValue("")
					m.branchInput.Focus()
					return m, nil
				}
				m.errMsg = "Not implemented yet."
				m.mode = modeList
				m.actionIndex = 0
				m.actionBranch = ""
				return m, nil
			}
			return m, nil
		}
		if m.mode == modeBranchPick {
			switch msg.String() {
			case "esc":
				m.mode = modeList
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
				m.errMsg = "Not implemented yet."
				m.mode = modeList
				m.branchInput.Blur()
				m.branchSuggestions = nil
				m.branchIndex = 0
				return m, nil
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
			return m, fetchStatusCmd(m.mgr)
		case "enter":
			if isCreateRow(m.table.Cursor(), m.status) {
				m.mode = modeCreate
				m.input.Focus()
				m.errMsg = ""
				return m, nil
			}
			if row, ok := selectedWorktree(m.status, m.table.Cursor()); ok {
				m.mode = modeAction
				m.actionBranch = row.Branch
				m.actionIndex = 0
				m.errMsg = ""
				return m, nil
			}
		case "d":
			if row, ok := selectedWorktree(m.status, m.table.Cursor()); ok {
				m.mode = modeDelete
				m.deletePath = row.Path
				m.deleteBranch = row.Branch
				m.errMsg = ""
				return m, nil
			}
		}
	}
	var cmd tea.Cmd
	m.table, cmd = m.table.Update(msg)
	return m, cmd
}

func (m model) View() string {
	var b strings.Builder
	b.WriteString(bannerStyle.Render("WTX"))
	b.WriteString("\n\n")

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

	if m.mode == modeCreate {
		b.WriteString("New worktree branch:\n")
		b.WriteString(inputStyle.Render(m.input.View()))
		b.WriteString("\n")
		if m.errMsg != "" {
			b.WriteString(errorStyle.Render(m.errMsg))
			b.WriteString("\n")
		}
		b.WriteString("\nPress enter to create, esc to cancel.\n")
		return b.String()
	}
	if m.mode == modeAction {
		b.WriteString("Worktree actions:\n")
		for i, item := range actionItems(m.actionBranch, m.status.BaseRef) {
			line := "  " + actionNormalStyle.Render(item)
			if i == m.actionIndex {
				line = "  " + actionSelectedStyle.Render(item)
			}
			b.WriteString(line + "\n")
		}
		b.WriteString("\nPress enter to select, esc to cancel.\n")
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
		b.WriteString("\nPress enter to select, esc to cancel.\n")
		return b.String()
	}
	if m.mode == modeDelete {
		b.WriteString("Delete worktree:\n")
		b.WriteString(fmt.Sprintf("%s\n", m.deleteBranch))
		b.WriteString(fmt.Sprintf("%s\n", m.deletePath))
		b.WriteString("\nAre you sure? (y/N)\n")
		return b.String()
	}
	b.WriteString(baseStyle.Render(m.table.View()))
	if m.status.Err != nil {
		b.WriteString(errorStyle.Render(fmt.Sprintf("Error: %v", m.status.Err)))
		b.WriteString("\n")
	}
	if m.errMsg != "" {
		b.WriteString(errorStyle.Render(m.errMsg))
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
	selectedPath := currentWorktreePath(m.status, m.table.Cursor())
	if selectedPath != "" {
		b.WriteString("\n")
		b.WriteString(secondaryStyle.Render(selectedPath))
		b.WriteString("\n")
	}
	b.WriteString("\n")
	help := "Press q to quit."
	if _, ok := selectedWorktree(m.status, m.table.Cursor()); ok {
		help = "Press enter for actions, d to delete, q to quit."
	}
	b.WriteString(help + "\n")
	return b.String()
}

type statusMsg WorktreeStatus

func fetchStatusCmd(mgr *WorktreeManager) tea.Cmd {
	return func() tea.Msg {
		return statusMsg(mgr.Status())
	}
}

func newTable() table.Model {
	columns := []table.Column{
		{Title: "Branch", Width: 28},
		{Title: "Status", Width: 10},
		{Title: "PR", Width: 6},
		{Title: "CI", Width: 6},
		{Title: "Approved", Width: 9},
	}
	t := table.New(
		table.WithColumns(columns),
		table.WithFocused(true),
		table.WithHeight(10),
	)
	t.SetStyles(tableStyles())
	return t
}

func worktreeRows(status WorktreeStatus) []table.Row {
	if !status.InRepo {
		return nil
	}
	orphaned := make(map[string]bool, len(status.Orphaned))
	for _, wt := range status.Orphaned {
		orphaned[wt.Path] = true
	}
	rows := make([]table.Row, 0, len(status.Worktrees))
	for _, wt := range status.Worktrees {
		label := wt.Branch
		if orphaned[wt.Path] {
			label = disabledStyle.Render(fmt.Sprintf("%s (orphaned)", wt.Branch))
		}
		statusLabel := "Free"
		if strings.Contains(strings.ToLower(wt.Branch), "main") {
			statusLabel = "In use"
		}
		pr := greenCheck()
		ci := redX()
		approved := greenCheck()
		rows = append(rows, table.Row{label, statusLabel, pr, ci, approved})
	}
	rows = append(rows, table.Row{"+ New worktree", "", "", "", ""})
	return rows
}

func tableStyles() table.Styles {
	s := table.DefaultStyles()
	s.Header = s.Header.
		BorderBottom(false).
		Bold(true).
		Foreground(lipgloss.Color("15")) // primary text
	s.Selected = s.Selected.
		Foreground(lipgloss.Color("15")). // primary text
		Background(lipgloss.Color("8")).  // selected background
		Bold(true)
	s.Cell = s.Cell.
		Foreground(lipgloss.Color("251")) // secondary text
	return s
}

var (
	baseStyle  = lipgloss.NewStyle()
	bannerStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#FFF7DB")).
			Background(lipgloss.Color("#7D56F4")).
			Padding(0, 1)
	errorStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("1")).Bold(true)
	secondaryStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
	disabledStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("245")).Faint(true)
	actionNormalStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("251"))
	actionSelectedStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("15")).Background(lipgloss.Color("8")).Bold(true)
	branchStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("15")).Bold(true)
	inputStyle = lipgloss.NewStyle().
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
	modeList uiMode = iota
	modeCreate
	modeDelete
	modeAction
	modeBranchPick
)

func newTextInput() textinput.Model {
	ti := textinput.New()
	ti.Placeholder = "feature/my-branch"
	ti.CharLimit = 200
	ti.Width = 40
	return ti
}

func newBranchInput() textinput.Model {
	ti := textinput.New()
	ti.Placeholder = "branch name"
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
	return cursor == len(status.Worktrees)
}

func selectedWorktree(status WorktreeStatus, cursor int) (WorktreeInfo, bool) {
	if !status.InRepo {
		return WorktreeInfo{}, false
	}
	if cursor < 0 || cursor >= len(status.Worktrees) {
		return WorktreeInfo{}, false
	}
	return status.Worktrees[cursor], true
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
		"Use " + branchStyle.Render(branch),
		"Checkout new branch from " + branchStyle.Render(base),
		"Choose an existing branch",
		"Open shell here",
	}
}

func currentWorktreePath(status WorktreeStatus, cursor int) string {
	if !status.InRepo {
		return ""
	}
	if cursor < 0 || cursor >= len(status.Worktrees) {
		return ""
	}
	return status.Worktrees[cursor].Path
}

func greenCheck() string {
	return "✓"
}

func redX() string {
	return "✗"
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
