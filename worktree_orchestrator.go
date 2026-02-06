package main

import "strings"

type WorktreeOrchestrator struct {
	mgr     *WorktreeManager
	lockMgr *LockManager
	prMgr   *GHManager
}

func NewWorktreeOrchestrator(mgr *WorktreeManager, lockMgr *LockManager, prMgr *GHManager) *WorktreeOrchestrator {
	return &WorktreeOrchestrator{mgr: mgr, lockMgr: lockMgr, prMgr: prMgr}
}

func (o *WorktreeOrchestrator) Status() WorktreeStatus {
	if o == nil || o.mgr == nil {
		return WorktreeStatus{}
	}
	status := o.mgr.ListForStatusBase()
	if status.Err != nil || !status.InRepo || strings.TrimSpace(status.RepoRoot) == "" || o.lockMgr == nil {
		return status
	}

	orphaned := make([]WorktreeInfo, 0)
	for _, wt := range status.Worktrees {
		exists, err := worktreePathExists(wt.Path)
		if err != nil {
			status.Err = err
			return status
		}
		if !exists {
			wt.Available = false
			for i := range status.Worktrees {
				if status.Worktrees[i].Path == wt.Path {
					status.Worktrees[i].Available = false
					status.Worktrees[i].LastUsedUnix = 0
					break
				}
			}
			orphaned = append(orphaned, wt)
			continue
		}
		lastUsed := worktreeLastUsedUnix(status.RepoRoot, wt.Path)
		available, err := o.lockMgr.IsAvailable(status.RepoRoot, wt.Path)
		if err != nil {
			status.Err = err
			return status
		}
		for i := range status.Worktrees {
			if status.Worktrees[i].Path == wt.Path {
				status.Worktrees[i].Available = available
				status.Worktrees[i].LastUsedUnix = lastUsed
				break
			}
		}
	}
	status.Orphaned = orphaned
	return status
}

func (o *WorktreeOrchestrator) PRDataForStatusWithError(status WorktreeStatus, force bool) (map[string]PRData, error) {
	if o == nil || o.prMgr == nil {
		return map[string]PRData{}, nil
	}
	if !status.InRepo || strings.TrimSpace(status.RepoRoot) == "" {
		return map[string]PRData{}, nil
	}
	branches := make([]string, 0, len(status.Worktrees))
	for _, wt := range status.Worktrees {
		b := strings.TrimSpace(wt.Branch)
		if b == "" || b == "detached" {
			continue
		}
		branches = append(branches, b)
	}
	if force {
		return o.prMgr.PRDataByBranchForce(status.RepoRoot, branches)
	}
	return o.prMgr.PRDataByBranch(status.RepoRoot, branches)
}

func (o *WorktreeOrchestrator) PRsForStatusWithError(status WorktreeStatus, force bool, enriched bool) ([]PRListData, error) {
	if o == nil || o.prMgr == nil {
		return []PRListData{}, nil
	}
	if !status.InRepo || strings.TrimSpace(status.RepoRoot) == "" {
		return []PRListData{}, nil
	}
	if enriched {
		return o.prMgr.PRsEnriched(status.RepoRoot, force)
	}
	return o.prMgr.PRs(status.RepoRoot, force)
}
