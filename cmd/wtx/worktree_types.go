package main

type WorktreeInfo struct {
	Path                string
	Branch              string
	Available           bool
	LastUsedUnix        int64
	PRURL               string
	PRNumber            int
	HasPR               bool
	PRStatus            string
	CIState             PRCIState
	CIDone              int
	CITotal             int
	CIFailingNames      string
	Approved            bool
	ReviewApproved      int
	ReviewRequired      int
	ReviewKnown         bool
	UnresolvedComments  int
	ResolvedComments    int
	CommentThreadsTotal int
	CommentsKnown       bool
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
