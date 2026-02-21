package main

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"
)

func newRootCommand(args []string) *cobra.Command {
	root := &cobra.Command{
		Use:           "wtx",
		Short:         "Interactive Git worktree picker",
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(_ *cobra.Command, _ []string) error {
			return runDefault(args)
		},
	}

	root.AddCommand(
		newConfigCommand(),
		newCheckoutCommand(),
		newCompletionCommand(),
		newTmuxStatusCommand(),
		newTmuxTitleCommand(),
		newTmuxAgentStartCommand(),
		newTmuxAgentExitCommand(),
		newShellCommand(),
		newIDECommand(),
		newIDEPickerCommand(),
	)

	if len(args) > 1 {
		root.SetArgs(args[1:])
	}
	return root
}

func newConfigCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "config",
		Short: "Open interactive configuration",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			p := tea.NewProgram(newConfigModel(), tea.WithMouseCellMotion())
			return p.Start()
		},
	}
}

func newTmuxStatusCommand() *cobra.Command {
	var worktree string
	cmd := &cobra.Command{
		Use:    "tmux-status",
		Short:  "Render tmux status line text",
		Args:   cobra.NoArgs,
		Hidden: true,
		RunE: func(_ *cobra.Command, _ []string) error {
			return runTmuxStatus([]string{"--worktree", worktree})
		},
	}
	cmd.Flags().StringVar(&worktree, "worktree", "", "Worktree path")
	return cmd
}

func newTmuxTitleCommand() *cobra.Command {
	var worktree string
	cmd := &cobra.Command{
		Use:    "tmux-title",
		Short:  "Render tmux title text",
		Args:   cobra.NoArgs,
		Hidden: true,
		RunE: func(_ *cobra.Command, _ []string) error {
			return runTmuxTitle([]string{"--worktree", worktree})
		},
	}
	cmd.Flags().StringVar(&worktree, "worktree", "", "Worktree path")
	return cmd
}

func newTmuxAgentStartCommand() *cobra.Command {
	var worktree string
	cmd := &cobra.Command{
		Use:    "tmux-agent-start",
		Short:  "Mark tmux agent as running",
		Args:   cobra.NoArgs,
		Hidden: true,
		RunE: func(_ *cobra.Command, _ []string) error {
			return runTmuxAgentStart([]string{"--worktree", worktree})
		},
	}
	cmd.Flags().StringVar(&worktree, "worktree", "", "Worktree path")
	return cmd
}

func newTmuxAgentExitCommand() *cobra.Command {
	var worktree string
	var code int
	cmd := &cobra.Command{
		Use:    "tmux-agent-exit",
		Short:  "Mark tmux agent as exited",
		Args:   cobra.NoArgs,
		Hidden: true,
		RunE: func(_ *cobra.Command, _ []string) error {
			return runTmuxAgentExit([]string{
				"--worktree", worktree,
				"--code", fmt.Sprintf("%d", code),
			})
		},
	}
	cmd.Flags().StringVar(&worktree, "worktree", "", "Worktree path")
	cmd.Flags().IntVar(&code, "code", 0, "Agent exit code")
	return cmd
}

func newShellCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "shell",
		Short: "Open a tmux shell split in current directory",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			return runShell()
		},
	}
}

func newIDECommand() *cobra.Command {
	return &cobra.Command{
		Use:   "ide [path]",
		Short: "Open IDE for an optional path",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(_ *cobra.Command, cmdArgs []string) error {
			return runIDE(cmdArgs)
		},
	}
}

func newIDEPickerCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "ide-picker [base-path]",
		Short: "Open interactive directory picker, then launch IDE",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(_ *cobra.Command, cmdArgs []string) error {
			return runIDEPicker(cmdArgs)
		},
	}
}

func runDefault(args []string) error {
	handled, err := ensureFreshTmuxSession(args)
	if err != nil {
		return err
	}
	if handled {
		return nil
	}

	exists, err := ConfigExists()
	if err != nil {
		return err
	}
	if !exists {
		return fmt.Errorf("wtx not configured. run: wtx config")
	}

	setITermWTXTab()
	setStartupStatusBanner()

	shouldResetTabColor := true
	defer func() {
		if shouldResetTabColor {
			resetITermTabColor()
		}
	}()

	p := tea.NewProgram(newModel(), tea.WithMouseCellMotion())
	finalModel, err := p.Run()
	if err != nil {
		return err
	}
	if m, ok := finalModel.(model); ok {
		path, branch, openShell, lock := m.PendingWorktree()
		if strings.TrimSpace(path) != "" {
			shouldResetTabColor = false
			runner := NewRunner(NewLockManager())
			if openShell {
				if _, err := runner.RunShellInWorktree(path, branch, lock); err != nil {
					if lock != nil {
						lock.Release()
					}
					return err
				}
			} else {
				if _, err := runner.RunInWorktree(path, branch, lock); err != nil {
					if lock != nil {
						lock.Release()
					}
					return err
				}
			}
		}
	}
	return nil
}
