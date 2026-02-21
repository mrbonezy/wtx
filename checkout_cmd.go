package main

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
)

func newCheckoutCommand(args []string) *cobra.Command {
	var baseRef string
	var fetch bool
	var noFetch bool

	cmd := &cobra.Command{
		Use:     "co <branch>",
		Aliases: []string{"checkout"},
		Short:   "Non-interactive checkout/open flow",
		Args:    cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, cmdArgs []string) error {
			return runCheckout(args, cmdArgs[0], baseRef, fetch, noFetch)
		},
	}
	cmd.Flags().StringVar(&baseRef, "base", "", "Base ref to use when creating a new branch")
	cmd.Flags().BoolVar(&fetch, "fetch", false, "Fetch before creating a new branch")
	cmd.Flags().BoolVar(&noFetch, "no-fetch", false, "Skip fetch before creating a new branch")
	return cmd
}

func runCheckout(args []string, branch string, explicitBaseRef string, fetch bool, noFetch bool) error {
	handled, err := ensureFreshTmuxSession(args)
	if err != nil {
		return err
	}
	if handled {
		return nil
	}

	branch = strings.TrimSpace(branch)
	if branch == "" {
		return errors.New("branch name required")
	}

	cfg, err := loadConfigForCheckout()
	if err != nil {
		return err
	}
	explicitFetch, err := explicitFetchPreference(fetch, noFetch)
	if err != nil {
		return err
	}
	doFetch := resolveCheckoutFetchPreference(explicitFetch, cfg)

	lockMgr := NewLockManager()
	mgr := NewWorktreeManager("", lockMgr)
	orchestrator := NewWorktreeOrchestrator(mgr, lockMgr, NewGHManager())
	status := orchestrator.Status()
	if status.Err != nil {
		return status.Err
	}
	if !status.GitInstalled {
		return errGitNotInstalled
	}
	if !status.InRepo {
		return errNotInGitRepository
	}

	gitPath, repoRoot, err := requireGitContext(status.CWD)
	if err != nil {
		return err
	}
	targetIsNew := !localBranchExists(repoRoot, gitPath, branch)
	baseRef := resolveCheckoutBaseRef(explicitBaseRef, cfg, status)

	targetPath, targetBranch, lock, err := resolveCheckoutTarget(mgr, orchestrator, status, branch, baseRef, targetIsNew, doFetch)
	if err != nil {
		return err
	}

	runner := NewRunner(lockMgr)
	fmt.Fprintln(os.Stdout, formatRunCommandMessage(cfg.AgentCommand))
	if _, err := runner.RunInWorktree(targetPath, targetBranch, lock); err != nil {
		if lock != nil {
			lock.Release()
		}
		return err
	}
	return nil
}

func loadConfigForCheckout() (Config, error) {
	cfg, err := LoadConfig()
	if err == nil {
		return cfg, nil
	}
	exists, statErr := ConfigExists()
	if statErr != nil {
		return Config{}, statErr
	}
	if exists {
		return Config{}, err
	}
	return Config{AgentCommand: defaultAgentCommand}, nil
}

func explicitFetchPreference(fetch bool, noFetch bool) (*bool, error) {
	if fetch && noFetch {
		return nil, errors.New("--fetch and --no-fetch are mutually exclusive")
	}
	if fetch {
		v := true
		return &v, nil
	}
	if noFetch {
		v := false
		return &v, nil
	}
	return nil, nil
}

func resolveCheckoutFetchPreference(explicit *bool, cfg Config) bool {
	if explicit != nil {
		return *explicit
	}
	if cfg.NewBranchFetchFirst != nil {
		return *cfg.NewBranchFetchFirst
	}
	return true
}

func resolveCheckoutBaseRef(explicitBaseRef string, cfg Config, status WorktreeStatus) string {
	baseRef := strings.TrimSpace(explicitBaseRef)
	if baseRef != "" {
		return baseRef
	}
	if v := strings.TrimSpace(cfg.NewBranchBaseRef); v != "" {
		return v
	}
	if v := strings.TrimSpace(status.BaseRef); v != "" {
		return v
	}
	return "origin/main"
}

func resolveCheckoutTarget(
	mgr *WorktreeManager,
	orchestrator *WorktreeOrchestrator,
	status WorktreeStatus,
	branch string,
	baseRef string,
	targetIsNew bool,
	doFetch bool,
) (path string, targetBranch string, lock *WorktreeLock, err error) {
	slots := checkoutSlotsFromStatus(status)
	if slot, ok := orchestrator.ResolveOpenTargetSlot(slots, branch, targetIsNew); ok {
		if targetIsNew {
			lock, err = mgr.AcquireWorktreeLock(slot.Path)
			if err != nil {
				return "", "", nil, err
			}
			if err := mgr.CheckoutNewBranch(slot.Path, branch, baseRef, doFetch); err != nil {
				lock.Release()
				return "", "", nil, err
			}
			return slot.Path, branch, lock, nil
		}
		if strings.TrimSpace(slot.Branch) == branch {
			lock, err = mgr.AcquireWorktreeLock(slot.Path)
			if err != nil {
				return "", "", nil, err
			}
			return slot.Path, branch, lock, nil
		}
		lock, err = mgr.AcquireWorktreeLock(slot.Path)
		if err != nil {
			return "", "", nil, err
		}
		if err := mgr.CheckoutExistingBranch(slot.Path, branch); err != nil {
			lock.Release()
			return "", "", nil, err
		}
		return slot.Path, branch, lock, nil
	}

	if targetIsNew {
		if doFetch {
			if err := mgr.FetchRepo(); err != nil {
				return "", "", nil, err
			}
		}
		created, err := mgr.CreateWorktree(branch, baseRef)
		if err != nil {
			return "", "", nil, err
		}
		lock, err = mgr.AcquireWorktreeLock(created.Path)
		if err != nil {
			return "", "", nil, err
		}
		return created.Path, branch, lock, nil
	}

	created, err := mgr.CreateWorktreeFromBranch(branch)
	if err != nil {
		return "", "", nil, err
	}
	lock, err = mgr.AcquireWorktreeLock(created.Path)
	if err != nil {
		return "", "", nil, err
	}
	return created.Path, branch, lock, nil
}

func checkoutSlotsFromStatus(status WorktreeStatus) []openSlotState {
	slots := make([]openSlotState, 0, len(status.Worktrees))
	for _, wt := range status.Worktrees {
		dirty := false
		if wt.Available {
			d, err := worktreeDirty(wt.Path)
			if err != nil {
				dirty = true
			} else {
				dirty = d
			}
		}
		slots = append(slots, openSlotState{
			Path:   wt.Path,
			Branch: wt.Branch,
			Locked: !wt.Available,
			Dirty:  dirty,
		})
	}
	return slots
}

func formatRunCommandMessage(agentCommand string) string {
	cmd := strings.TrimSpace(agentCommand)
	if cmd == "" {
		cmd = defaultAgentCommand
	}
	return "Running " + cmd
}
