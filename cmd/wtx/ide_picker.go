package main

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type dirPickerModel struct {
	basePath    string
	ideCmd      string
	entries     []string
	filtered    []string
	selected    int
	filter      textinput.Model
	cancelled   bool
	chosenPath  string
}

func newDirPickerModel(basePath string, ideCmd string) dirPickerModel {
	ti := textinput.New()
	ti.Placeholder = "type to filter..."
	ti.Focus()
	ti.CharLimit = 256
	ti.Width = 50

	entries := listDirectories(basePath, 1)

	return dirPickerModel{
		basePath: basePath,
		ideCmd:   ideCmd,
		entries:  entries,
		filtered: entries,
		selected: 0,
		filter:   ti,
	}
}

func listDirectories(basePath string, maxDepth int) []string {
	var dirs []string
	dirs = append(dirs, "(root)")

	err := filepath.WalkDir(basePath, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if !d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(basePath, path)
		if err != nil {
			return nil
		}
		if rel == "." {
			return nil
		}
		// Skip hidden directories
		if strings.HasPrefix(d.Name(), ".") {
			return filepath.SkipDir
		}
		// Check depth
		depth := strings.Count(rel, string(os.PathSeparator)) + 1
		if depth > maxDepth {
			return filepath.SkipDir
		}
		dirs = append(dirs, rel)
		return nil
	})
	if err != nil {
		return dirs
	}

	if len(dirs) > 1 {
		sort.Strings(dirs[1:])
	}
	return dirs
}

func (m dirPickerModel) Init() tea.Cmd {
	return textinput.Blink
}

func (m dirPickerModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "esc":
			m.cancelled = true
			return m, tea.Quit
		case "enter":
			if len(m.filtered) > 0 && m.selected < len(m.filtered) {
				selected := m.filtered[m.selected]
				if selected == "(root)" {
					m.chosenPath = m.basePath
				} else {
					m.chosenPath = filepath.Join(m.basePath, selected)
				}
			} else {
				m.chosenPath = m.basePath
			}
			return m, tea.Quit
		case "up":
			if m.selected > 0 {
				m.selected--
			}
			return m, nil
		case "down":
			if m.selected < len(m.filtered)-1 {
				m.selected++
			}
			return m, nil
		}
	}

	var cmd tea.Cmd
	m.filter, cmd = m.filter.Update(msg)

	// Filter entries based on input (prefix match)
	query := strings.ToLower(m.filter.Value())
	if query == "" {
		m.filtered = m.entries
	} else {
		var matches []string
		for _, e := range m.entries {
			if strings.HasPrefix(strings.ToLower(e), query) {
				matches = append(matches, e)
			}
		}
		m.filtered = matches
	}

	// Keep selection in bounds
	if m.selected >= len(m.filtered) {
		m.selected = len(m.filtered) - 1
	}
	if m.selected < 0 {
		m.selected = 0
	}

	return m, cmd
}

func (m dirPickerModel) View() string {
	var b strings.Builder

	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#7D56F4"))
	selectedStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#7D56F4")).Bold(true)
	normalStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("251"))
	dimStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("245"))

	b.WriteString(titleStyle.Render("Open in \"" + m.ideCmd + "\""))
	b.WriteString("\n")
	b.WriteString(m.filter.View())
	b.WriteString("\n")
	b.WriteString(dimStyle.Render("────────────────────────────────────────"))
	b.WriteString("\n")

	maxVisible := 12
	start := 0
	if m.selected >= maxVisible {
		start = m.selected - maxVisible + 1
	}

	for i := start; i < len(m.filtered) && i < start+maxVisible; i++ {
		entry := m.filtered[i]
		if i == m.selected {
			b.WriteString(selectedStyle.Render("> " + entry))
		} else {
			b.WriteString(normalStyle.Render("  " + entry))
		}
		b.WriteString("\n")
	}

	if len(m.filtered) == 0 {
		b.WriteString(dimStyle.Render("  (no matches)"))
		b.WriteString("\n")
	}

	b.WriteString("\n")
	b.WriteString(dimStyle.Render("↑/↓ navigate • enter select • esc cancel"))

	return b.String()
}

func runIDEPicker(args []string) error {
	var basePath string
	if len(args) > 0 {
		basePath = strings.TrimSpace(args[0])
	}
	if basePath == "" {
		basePath, _ = os.Getwd()
	}

	cfg, err := LoadConfig()
	if err != nil {
		return err
	}
	ideCmd := strings.TrimSpace(cfg.IDECommand)
	if ideCmd == "" {
		ideCmd = defaultIDECommand
	}

	p := tea.NewProgram(newDirPickerModel(basePath, ideCmd))
	finalModel, err := p.Run()
	if err != nil {
		return err
	}

	m := finalModel.(dirPickerModel)
	if m.cancelled || m.chosenPath == "" {
		return nil
	}

	return runIDE([]string{m.chosenPath})
}
