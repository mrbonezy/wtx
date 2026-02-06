package main

type WorktreeInfo struct {
	Path               string
	Branch             string
	Available          bool
	LastUsedUnix       int64
	PRURL              string
	PRNumber           int
	HasPR              bool
	PRStatus           string
	CIState            PRCIState
	CIDone             int
	CITotal            int
	Approved           bool
	ReviewApproved     int
	ReviewRequired     int
	ReviewKnown        bool
	UnresolvedComments int
}

type WorktreeStatus struct {
	GitInstalled bool
	InRepo       bool
	RepoRoot     string
	CWD          string
	BaseRef      string
	Worktrees    []WorktreeInfo
	Orphaned     []WorktreeInfo
	Malformed    []string
	Err          error
}
