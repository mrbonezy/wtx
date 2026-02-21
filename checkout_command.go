package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"
)

const checkoutStepSpinnerDelay = 0 * time.Millisecond

func newCheckoutCommand() *cobra.Command {
	var create bool
	var baseOverride string
	var fetch bool
	var noFetch bool

	cmd := &cobra.Command{
		Use:     "checkout <existing_branch>",
		Aliases: []string{"co"},
		Short:   "Select or create a branch worktree and switch into it",
		Long: "Behaves like interactive branch selection.\n\n" +
			"Without -b, <existing_branch> must already exist.\n" +
			"With -b, the argument is treated as a new branch name and fails if it exists locally or on any remote.\n" +
			"--from, --fetch and --no-fetch are only valid with -b.",
		Example: strings.Join([]string{
			"  wtx checkout feature/auth-flow",
			"  wtx co bugfix/login-timeout",
			"  wtx checkout -b feature/new-api",
			"  wtx checkout -b feature/new-api --from origin/main --fetch",
		}, "\n"),
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) == 1 {
				return nil
			}
			if len(args) == 0 {
				return usageError(cmd, "missing branch argument")
			}
			return usageError(cmd, "too many arguments; provide exactly one branch name")
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			if fetch && noFetch {
				return usageError(cmd, "--fetch and --no-fetch cannot be used together")
			}
			if !create && (strings.TrimSpace(baseOverride) != "" || fetch || noFetch) {
				return usageError(cmd, "--from, --fetch and --no-fetch require -b")
			}

			var fetchOverride *bool
			if fetch {
				v := true
				fetchOverride = &v
			}
			if noFetch {
				v := false
				fetchOverride = &v
			}

			return runCheckout(args[0], create, baseOverride, fetchOverride, os.Args)
		},
	}

	cmd.Flags().BoolVarP(&create, "create", "b", false, "Create a new branch")
	cmd.Flags().StringVar(&baseOverride, "from", "", "Base branch/ref for one-time branch creation (requires -b)")
	cmd.Flags().BoolVar(&fetch, "fetch", false, "Fetch before one-time branch creation (requires -b)")
	cmd.Flags().BoolVar(&noFetch, "no-fetch", false, "Do not fetch before one-time branch creation (requires -b)")
	cmd.ValidArgsFunction = checkoutBranchCompletion
	_ = cmd.RegisterFlagCompletionFunc("from", checkoutFromCompletion)
	return cmd
}

func usageError(cmd *cobra.Command, message string) error {
	return fmt.Errorf("%s\n\n%s", message, strings.TrimSpace(cmd.UsageString()))
}

func checkoutBranchCompletion(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	if len(args) > 0 {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	create, err := cmd.Flags().GetBool("create")
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	if create {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	return completeBranchSuggestions(toComplete), cobra.ShellCompDirectiveNoFileComp
}

func checkoutFromCompletion(cmd *cobra.Command, _ []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	create, err := cmd.Flags().GetBool("create")
	if err != nil || !create {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	return completeBranchSuggestions(toComplete), cobra.ShellCompDirectiveNoFileComp
}

func runCheckout(branch string, create bool, baseOverride string, fetchOverride *bool, args []string) error {
	branch = strings.TrimSpace(branch)
	if branch == "" {
		return errors.New("branch name required")
	}

	exists, err := ConfigExists()
	if err != nil {
		return err
	}
	if !exists {
		return fmt.Errorf("wtx not configured. run: wtx config")
	}

	lockMgr := NewLockManager()
	mgr := NewWorktreeManager("", lockMgr)
	orchestrator := NewWorktreeOrchestrator(mgr, lockMgr, NewGHManager())
	runner := NewRunner(lockMgr)

	var (
		status          WorktreeStatus
		gitPath, repoRoot string
		baseRef         string
		doFetch         bool
	)
	if err := runCheckoutStep("Preparing checkout", func() error {
		status = orchestrator.Status()
		if status.Err != nil {
			return status.Err
		}
		if !status.GitInstalled {
			return errGitNotInstalled
		}
		if !status.InRepo {
			return errNotInGitRepository
		}
		var err error
		gitPath, repoRoot, err = requireGitContext("")
		if err != nil {
			return err
		}
		exists, err := branchExistsLocalOrRemote(repoRoot, gitPath, branch)
		if err != nil {
			return err
		}
		if create && exists {
			return fmt.Errorf("branch %q already exists locally or on a remote", branch)
		}
		if !create && !exists {
			return fmt.Errorf("branch %q does not exist locally or on known remote-tracking refs", branch)
		}
		baseRef, doFetch = checkoutDefaults(status)
		if create {
			if v := strings.TrimSpace(baseOverride); v != "" {
				baseRef = v
			}
			if fetchOverride != nil {
				doFetch = *fetchOverride
			}
			if err := validateCreateCheckoutBaseRef(repoRoot, gitPath, baseRef, doFetch); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		return err
	}

	handled, err := ensureFreshTmuxSession(args)
	if err != nil {
		return err
	}
	if handled {
		return nil
	}
	setStartupStatusBanner()

	var slots []openSlotState
	if err := runCheckoutStep("Preparing worktree", func() error {
		var err error
		slots, err = loadOpenSlotsForCheckout(orchestrator, status)
		return err
	}); err != nil {
		return err
	}

	target := model{
		mgr:               mgr,
		openTargetBranch:  branch,
		openTargetIsNew:   create,
		openTargetBaseRef: baseRef,
		openTargetFetch:   doFetch,
	}

	var openResult openUseReadyMsg
	if slot, ok := orchestrator.ResolveOpenTargetSlot(slots, branch, create); ok {
		if err := runCheckoutStep("Switching worktree", func() error {
			var err error
			openResult, err = runOpenSelectionCmd(openCmdForTargetOnSlot(target, slot))
			return err
		}); err != nil {
			return err
		}
	} else {
		fmt.Fprintln(os.Stderr, "No worktree is available for this target branch.")
		createNew, err := promptCreateWorktree(branch)
		if err != nil {
			return err
		}
		if !createNew {
			return nil
		}
		if err := runCheckoutStep("Creating worktree", func() error {
			var err error
			openResult, err = runOpenSelectionCmd(openCmdForCreateTarget(target))
			return err
		}); err != nil {
			return err
		}
	}

	if openResult.err != nil {
		return openResult.err
	}
	if strings.TrimSpace(openResult.path) == "" {
		return errors.New("checkout did not resolve a worktree")
	}

	setITermWTXTab()
	setStartupStatusBanner()

	shouldResetTabColor := true
	defer func() {
		if shouldResetTabColor {
			resetITermTabColor()
		}
	}()

	shouldResetTabColor = false
	if err := runCheckoutStep("Launching agent", func() error {
		_, err := runner.RunInWorktree(openResult.path, openResult.branch, openResult.lock)
		return err
	}); err != nil {
		if openResult.lock != nil {
			openResult.lock.Release()
		}
		return err
	}
	return nil
}

func checkoutDefaults(status WorktreeStatus) (string, bool) {
	base := strings.TrimSpace(status.BaseRef)
	if base == "" {
		base = "origin/main"
	}
	fetch := true
	if cfg, err := LoadConfig(); err == nil {
		if v := strings.TrimSpace(cfg.NewBranchBaseRef); v != "" {
			base = v
		}
		if cfg.NewBranchFetchFirst != nil {
			fetch = *cfg.NewBranchFetchFirst
		}
	}
	return base, fetch
}

func runCheckoutStep(step string, fn func() error) error {
	step = strings.TrimSpace(step)
	if step == "" {
		step = "Working"
	}
	stop := startDelayedSpinner(step, checkoutStepSpinnerDelay)
	defer stop()
	return fn()
}

func loadOpenSlotsForCheckout(orchestrator *WorktreeOrchestrator, status WorktreeStatus) ([]openSlotState, error) {
	if orchestrator == nil {
		return []openSlotState{}, nil
	}
	slots := make([]openSlotState, len(status.Worktrees))
	for i, wt := range status.Worktrees {
		slot := openSlotState{
			Path:   wt.Path,
			Branch: wt.Branch,
			Locked: !wt.Available,
		}
		if locked, err := worktreeLockedByAny(orchestrator, status.RepoRoot, wt.Path); err == nil && locked {
			slot.Locked = true
		}
		if dirty, err := worktreeDirty(wt.Path); err == nil {
			slot.Dirty = dirty
		}
		slots[i] = slot
	}
	return slots, nil
}

func runOpenSelectionCmd(cmd tea.Cmd) (openUseReadyMsg, error) {
	if cmd == nil {
		return openUseReadyMsg{}, errors.New("open selection command unavailable")
	}
	msg := cmd()
	out, ok := msg.(openUseReadyMsg)
	if !ok {
		return openUseReadyMsg{}, fmt.Errorf("unexpected open selection result: %T", msg)
	}
	return out, nil
}

func promptCreateWorktree(branch string) (bool, error) {
	branch = strings.TrimSpace(branch)
	if branch == "" {
		branch = "branch"
	}
	fmt.Fprintf(os.Stderr, "Create a new worktree for %s? [y/N]: ", branch)
	reader := bufio.NewReader(os.Stdin)
	line, err := reader.ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return false, err
	}
	response := strings.ToLower(strings.TrimSpace(line))
	return response == "y" || response == "yes", nil
}

func validateCreateCheckoutBaseRef(repoRoot string, gitPath string, baseRef string, doFetch bool) error {
	baseRef = strings.TrimSpace(baseRef)
	if baseRef == "" {
		baseRef = "HEAD"
	}
	resolved := baseRefForWorktreeAdd(repoRoot, gitPath, baseRef)
	if _, err := gitOutputInDir(repoRoot, gitPath, "rev-parse", "--verify", resolved+"^{commit}"); err == nil {
		return nil
	}

	remotes, remoteErr := listGitRemotes(repoRoot, gitPath)
	if remoteErr == nil && len(remotes) == 0 {
		return fmt.Errorf("base ref %q is not valid in this repository and no remotes are configured; use --from <local-branch> or update defaults in `wtx config`", baseRef)
	}
	if !doFetch {
		return fmt.Errorf("base ref %q is not currently resolvable; use --fetch or set --from <base>", baseRef)
	}
	return nil
}
