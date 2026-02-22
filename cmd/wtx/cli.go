package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"
)

var installVersionFn = installVersion

func newRootCommand(args []string) *cobra.Command {
	var showVersion bool
	root := &cobra.Command{
		Use:           "wtx",
		Short:         "Interactive Git worktree picker",
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(_ *cobra.Command, _ []string) error {
			if showVersion {
				return runVersionCommand()
			}
			return runDefault(args)
		},
	}
	root.Flags().BoolVarP(&showVersion, "version", "v", false, "Print wtx version and exit")

	root.AddCommand(
		newCheckoutCommand(),
		newPRCommand(),
		newConfigCommand(),
		newCompletionCommand(),
		newUpdateCommand(),
		newTmuxStatusCommand(),
		newTmuxTitleCommand(),
		newTmuxAgentStartCommand(),
		newTmuxAgentExitCommand(),
		newTmuxActionsCommand(),
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
			if testModeEnabled() {
				fetch := true
				return SaveConfig(Config{
					AgentCommand:          defaultAgentCommand,
					NewBranchFetchFirst:   &fetch,
					IDECommand:            defaultIDECommand,
					MainScreenBranchLimit: defaultMainScreenBranchLimit,
				})
			}
			p := tea.NewProgram(newConfigModel(), tea.WithMouseCellMotion())
			return p.Start()
		},
	}
}

func newUpdateCommand() *cobra.Command {
	var checkOnly bool
	var quiet bool
	cmd := &cobra.Command{
		Use:   "update",
		Short: "Check for and install the latest wtx version",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			return runUpdateCommand(checkOnly, quiet)
		},
	}
	cmd.Flags().BoolVar(&checkOnly, "check", false, "Check for updates only")
	cmd.Flags().BoolVar(&quiet, "quiet", false, "Print machine-friendly output")
	return cmd
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

func newTmuxActionsCommand() *cobra.Command {
	var sourcePane string
	cmd := &cobra.Command{
		Use:    "tmux-actions [path] [action]",
		Short:  "Open tmux actions popup",
		Args:   cobra.MaximumNArgs(2),
		Hidden: true,
		RunE: func(_ *cobra.Command, cmdArgs []string) error {
			args := append([]string{}, cmdArgs...)
			if strings.TrimSpace(sourcePane) != "" {
				args = append([]string{"--source-pane", sourcePane}, args...)
			}
				return runTmuxActions(args)
			},
	}
	cmd.Flags().StringVar(&sourcePane, "source-pane", "", "tmux pane id that triggered the action")
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
	if testModeEnabled() {
		fmt.Println("wtx test mode: interactive UI bypassed")
		return nil
	}
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

func runVersionCommand() error {
	cur := currentVersion()
	fmt.Println(cur)

	ctx, cancel := context.WithTimeout(context.Background(), resolveUpdateTimeout)
	defer cancel()

	latest, err := resolveLatestVersionFn(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "wtx version check: %v\n", err)
		return nil
	}
	result := updateCheckResult{
		CurrentVersion:  cur,
		LatestVersion:   latest,
		UpdateAvailable: isUpdateAvailableForInstall(cur, latest),
	}
	printUpdateCheckResultTo(os.Stderr, result, false)
	if !result.UpdateAvailable || !isInteractiveTerminal(os.Stdin) || !isInteractiveTerminal(os.Stdout) {
		return nil
	}
	return promptAndMaybeInstallVersionUpdate(os.Stdin, os.Stdout, result)
}

func promptAndMaybeInstallVersionUpdate(r io.Reader, w io.Writer, result updateCheckResult) error {
	if !result.UpdateAvailable {
		return nil
	}
	fmt.Fprint(w, "Do you want to update now? [y/N]: ")
	reader := bufio.NewReader(r)
	answer, err := reader.ReadString('\n')
	if err != nil && err != io.EOF {
		return err
	}
	answer = strings.TrimSpace(strings.ToLower(answer))
	if answer != "y" && answer != "yes" {
		fmt.Fprintln(w, "Skipped update.")
		return nil
	}

	installCtx, installCancel := context.WithTimeout(context.Background(), installUpdateTimeout)
	defer installCancel()
	stopSpinner := startDelayedSpinner(fmt.Sprintf("Updating wtx to %s...", result.LatestVersion), 0)
	defer stopSpinner()
	if err := installVersionFn(installCtx, result.LatestVersion); err != nil {
		return err
	}
	fmt.Fprintf(w, "Updated wtx to %s\n", result.LatestVersion)
	return nil
}

func isInteractiveTerminal(f *os.File) bool {
	if f == nil {
		return false
	}
	info, err := f.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}
