package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

type PRCIState string

const (
	PRCINone       PRCIState = "none"
	PRCIInProgress PRCIState = "in_progress"
	PRCIFail       PRCIState = "fail"
	PRCISuccess    PRCIState = "success"

	ghPRListFullTimeout     = 30 * time.Second
	ghPRListFallbackTimeout = 20 * time.Second
	ghPRHeadFullTimeout     = 15 * time.Second
	ghPRHeadFallbackTimeout = 10 * time.Second
	ghUnresolvedPRTimeout   = 8 * time.Second
	ghProtectionTimeout     = 5 * time.Second
	ghReviewCountTimeout    = 6 * time.Second
	fullPRListFields        = "number,url,headRefName,baseRefName,title,isDraft,state,mergeStateStatus,updatedAt,mergedAt,reviewDecision,statusCheckRollup"
	fallbackPRListFields    = "number,url,headRefName,baseRefName,title,isDraft,state,mergeStateStatus,updatedAt,mergedAt,reviewDecision"
	defaultPRListFetchLimit = "15"
	maxBranchFetchParallel  = 6
	maxPREnrichmentParallel = 6
)

type PRData struct {
	Number              int
	URL                 string
	Branch              string
	Status              string
	ReviewDecision      string
	Approved            bool
	ReviewApproved      int
	ReviewRequired      int
	ReviewKnown         bool
	UnresolvedComments  int
	ResolvedComments    int
	CommentThreadsTotal int
	CIState             PRCIState
	CICompleted         int
	CITotal             int
	CIFailingNames      string
	CommentsKnown       bool
	BaseStatus          string
}

type PRListData struct {
	Number              int
	URL                 string
	Branch              string
	Title               string
	Status              string
	ReviewDecision      string
	Approved            bool
	ReviewApproved      int
	ReviewRequired      int
	ReviewKnown         bool
	CIState             PRCIState
	CICompleted         int
	CITotal             int
	CIFailingNames      string
	UnresolvedComments  int
	ResolvedComments    int
	CommentThreadsTotal int
	UpdatedAt           time.Time
	CommentsKnown       bool
	MergeStateStatus    string
	BaseStatus          string
}

type GHManager struct {
	mu                  sync.Mutex
	branchCache         map[string]map[string]cachedBranchPRData
	prListCache         map[string]cachedPRListData
	prListEnrichedCache map[string]cachedPRListData
	ttl                 time.Duration
}

type cachedBranchPRData struct {
	fetchedAt time.Time
	found     bool
	data      PRData
}

type cachedPRListData struct {
	fetchedAt time.Time
	prList    []PRListData
}

type ghPR struct {
	Number            int       `json:"number"`
	URL               string    `json:"url"`
	HeadRefName       string    `json:"headRefName"`
	Title             string    `json:"title"`
	IsDraft           bool      `json:"isDraft"`
	State             string    `json:"state"`
	MergeStateStatus  string    `json:"mergeStateStatus"`
	BaseRefName       string    `json:"baseRefName"`
	UpdatedAt         string    `json:"updatedAt"`
	MergedAt          string    `json:"mergedAt"`
	ReviewDecision    string    `json:"reviewDecision"`
	StatusCheckRollup []ghCheck `json:"statusCheckRollup"`
}

type ghCheck struct {
	Conclusion string `json:"conclusion"`
	Status     string `json:"status"`
	Name       string `json:"name"`
	Context    string `json:"context"`
}

type ghReviewThreadsResp struct {
	Data struct {
		Repository struct {
			PullRequest struct {
				ReviewThreads struct {
					TotalCount int `json:"totalCount"`
					PageInfo   struct {
						HasNextPage bool   `json:"hasNextPage"`
						EndCursor   string `json:"endCursor"`
					} `json:"pageInfo"`
					Nodes []struct {
						IsResolved bool `json:"isResolved"`
					} `json:"nodes"`
				} `json:"reviewThreads"`
			} `json:"pullRequest"`
		} `json:"repository"`
	} `json:"data"`
}

type ghBranchProtectionResp struct {
	RequiredPullRequestReviews *struct {
		RequiredApprovingReviewCount int `json:"required_approving_review_count"`
	} `json:"required_pull_request_reviews"`
}

type ghPullReview struct {
	State string `json:"state"`
	User  struct {
		Login string `json:"login"`
	} `json:"user"`
}

type requiredApprovalsInfo struct {
	count int
	known bool
}

func NewGHManager() *GHManager {
	return &GHManager{
		branchCache:         make(map[string]map[string]cachedBranchPRData),
		prListCache:         make(map[string]cachedPRListData),
		prListEnrichedCache: make(map[string]cachedPRListData),
		ttl:                 20 * time.Second,
	}
}

func (m *GHManager) PRDataByBranch(repoRoot string, branches []string) (map[string]PRData, error) {
	return m.prDataByBranch(repoRoot, branches, false)
}

func (m *GHManager) PRDataByBranchForce(repoRoot string, branches []string) (map[string]PRData, error) {
	return m.prDataByBranch(repoRoot, branches, true)
}

func (m *GHManager) PRs(repoRoot string, force bool) ([]PRListData, error) {
	repoRoot = strings.TrimSpace(repoRoot)
	if repoRoot == "" {
		return []PRListData{}, nil
	}
	prs, fetchErr := m.ensurePRList(repoRoot, force)
	out := make([]PRListData, len(prs))
	copy(out, prs)
	return out, fetchErr
}

func (m *GHManager) PRsEnriched(repoRoot string, force bool) ([]PRListData, error) {
	repoRoot = strings.TrimSpace(repoRoot)
	if repoRoot == "" {
		return []PRListData{}, nil
	}
	prs, fetchErr := m.ensurePRListEnriched(repoRoot, force)
	out := make([]PRListData, len(prs))
	copy(out, prs)
	return out, fetchErr
}

func (m *GHManager) prDataByBranch(repoRoot string, branches []string, force bool) (map[string]PRData, error) {
	repoRoot = strings.TrimSpace(repoRoot)
	if repoRoot == "" || len(branches) == 0 {
		return map[string]PRData{}, nil
	}
	needed := make([]string, 0, len(branches))
	seen := make(map[string]bool, len(branches))
	for _, branch := range branches {
		b := strings.TrimSpace(branch)
		if b == "" || b == "detached" || seen[b] {
			continue
		}
		seen[b] = true
		needed = append(needed, b)
	}
	if len(needed) == 0 {
		return map[string]PRData{}, nil
	}
	out := make(map[string]PRData, len(needed))
	toFetch := make([]string, 0, len(needed))
	now := time.Now()
	m.mu.Lock()
	repoCache := m.branchCache[repoRoot]
	for _, b := range needed {
		entry, ok := repoCache[b]
		if !force && ok && now.Sub(entry.fetchedAt) < m.ttl {
			if entry.found {
				out[b] = entry.data
			}
			continue
		}
		toFetch = append(toFetch, b)
	}
	m.mu.Unlock()

	var fetchErr error
	if len(toFetch) > 0 {
		fetched, err := m.fetchPRDataForBranches(repoRoot, toFetch)
		if err != nil {
			fetchErr = err
		}
		m.mu.Lock()
		if _, ok := m.branchCache[repoRoot]; !ok {
			m.branchCache[repoRoot] = make(map[string]cachedBranchPRData)
		}
		for _, b := range toFetch {
			data, found := fetched[b]
			m.branchCache[repoRoot][b] = cachedBranchPRData{
				fetchedAt: time.Now(),
				found:     found,
				data:      data,
			}
			if found {
				out[b] = data
			}
		}
		m.mu.Unlock()
	}

	m.mu.Lock()
	repoCache = m.branchCache[repoRoot]
	m.mu.Unlock()
	for _, b := range needed {
		if _, ok := out[b]; ok {
			continue
		}
		if entry, ok := repoCache[b]; ok && entry.found {
			out[b] = entry.data
		}
	}
	return out, fetchErr
}

func (m *GHManager) ensurePRList(repoRoot string, force bool) ([]PRListData, error) {
	now := time.Now()
	m.mu.Lock()
	cached, ok := m.prListCache[repoRoot]
	m.mu.Unlock()
	if !force && ok && now.Sub(cached.fetchedAt) < m.ttl {
		out := make([]PRListData, len(cached.prList))
		copy(out, cached.prList)
		return out, nil
	}
	prs, err := m.fetchRepoPRList(repoRoot)
	if err != nil {
		if ok {
			out := make([]PRListData, len(cached.prList))
			copy(out, cached.prList)
			return out, err
		}
		return []PRListData{}, err
	}
	m.mu.Lock()
	m.prListCache[repoRoot] = cachedPRListData{
		fetchedAt: time.Now(),
		prList:    prs,
	}
	m.mu.Unlock()
	out := make([]PRListData, len(prs))
	copy(out, prs)
	return out, nil
}

func (m *GHManager) ensurePRListEnriched(repoRoot string, force bool) ([]PRListData, error) {
	now := time.Now()
	m.mu.Lock()
	cached, ok := m.prListEnrichedCache[repoRoot]
	m.mu.Unlock()
	if !force && ok && now.Sub(cached.fetchedAt) < m.ttl {
		out := make([]PRListData, len(cached.prList))
		copy(out, cached.prList)
		return out, nil
	}
	prs, err := m.fetchRepoPRListEnriched(repoRoot)
	if err != nil {
		if ok {
			out := make([]PRListData, len(cached.prList))
			copy(out, cached.prList)
			return out, err
		}
		return []PRListData{}, err
	}
	m.mu.Lock()
	m.prListEnrichedCache[repoRoot] = cachedPRListData{
		fetchedAt: time.Now(),
		prList:    prs,
	}
	m.mu.Unlock()
	out := make([]PRListData, len(prs))
	copy(out, prs)
	return out, nil
}

func (m *GHManager) fetchRepoPRList(repoRoot string) ([]PRListData, error) {
	if _, err := exec.LookPath("gh"); err != nil {
		return nil, err
	}
	ghPath, err := exec.LookPath("gh")
	if err != nil {
		return nil, err
	}
	prs, err := ghPRList(ghPath, repoRoot, fullPRListFields, defaultPRListFetchLimit, ghPRListFullTimeout)
	if err != nil {
		// Large monorepos can fail this heavier query; retry once with a slimmer field set.
		prs, err = ghPRList(ghPath, repoRoot, fallbackPRListFields, defaultPRListFetchLimit, ghPRListFallbackTimeout)
		if err != nil {
			return nil, err
		}
	}
	prList := make([]PRListData, 0, len(prs))
	for _, pr := range prs {
		branch := strings.TrimSpace(pr.HeadRefName)
		if branch == "" {
			continue
		}
		updatedAt := parseGitHubTime(pr.UpdatedAt)
		ciState, ciDone, ciTotal, failingNames := summarizeCI(pr.StatusCheckRollup)
		baseStatus := normalizePRStatus(pr.State, pr.MergedAt, pr.IsDraft)
		reviewApproved, reviewRequired, reviewKnown := reviewProgressFromDecision(pr.ReviewDecision, strings.EqualFold(strings.TrimSpace(pr.ReviewDecision), "approved"))
		reviewSatisfied := hasSufficientApprovals(reviewApproved, reviewRequired, reviewKnown, pr.ReviewDecision, strings.EqualFold(strings.TrimSpace(pr.ReviewDecision), "approved"))
		status := computePRStatus(pr.State, pr.MergedAt, pr.IsDraft, pr.MergeStateStatus, reviewSatisfied, ciState, 0, false)
		prList = append(prList, PRListData{
			Number:              pr.Number,
			URL:                 strings.TrimSpace(pr.URL),
			Branch:              branch,
			Title:               strings.TrimSpace(pr.Title),
			Status:              status,
			ReviewDecision:      strings.TrimSpace(pr.ReviewDecision),
			Approved:            strings.EqualFold(strings.TrimSpace(pr.ReviewDecision), "approved"),
			ReviewApproved:      reviewApproved,
			ReviewRequired:      reviewRequired,
			ReviewKnown:         reviewKnown,
			CIState:             ciState,
			CICompleted:         ciDone,
			CITotal:             ciTotal,
			CIFailingNames:      failingNames,
			ResolvedComments:    0,
			CommentThreadsTotal: 0,
			UpdatedAt:           updatedAt,
			CommentsKnown:       false,
			MergeStateStatus:    strings.TrimSpace(pr.MergeStateStatus),
			BaseStatus:          baseStatus,
		})
	}
	sort.SliceStable(prList, func(i, j int) bool {
		iBucket := prStatusSortBucket(prList[i].Status)
		jBucket := prStatusSortBucket(prList[j].Status)
		if iBucket != jBucket {
			return iBucket < jBucket
		}
		if !prList[i].UpdatedAt.Equal(prList[j].UpdatedAt) {
			return prList[i].UpdatedAt.After(prList[j].UpdatedAt)
		}
		return prList[i].Number > prList[j].Number
	})
	return prList, nil
}

func (m *GHManager) fetchRepoPRListEnriched(repoRoot string) ([]PRListData, error) {
	if _, err := exec.LookPath("gh"); err != nil {
		return nil, err
	}
	ghPath, err := exec.LookPath("gh")
	if err != nil {
		return nil, err
	}
	prs, err := ghPRList(ghPath, repoRoot, fullPRListFields, defaultPRListFetchLimit, ghPRListFullTimeout)
	if err != nil {
		prs, err = ghPRList(ghPath, repoRoot, fallbackPRListFields, defaultPRListFetchLimit, ghPRListFallbackTimeout)
		if err != nil {
			return nil, err
		}
	}
	owner, name, err := resolveGitHubRepo(repoRoot)
	if err != nil {
		owner, name = "", ""
	}
	requiredByBase := map[string]requiredApprovalsInfo{}
	if owner != "" && name != "" {
		requiredByBase = fetchRequiredApprovalsByBaseRefs(ghPath, repoRoot, owner, name, prs)
	}
	prList := make([]PRListData, 0, len(prs))
	for _, pr := range prs {
		branch := strings.TrimSpace(pr.HeadRefName)
		if branch == "" {
			continue
		}
		updatedAt := parseGitHubTime(pr.UpdatedAt)
		ciState, ciDone, ciTotal, failingNames := summarizeCI(pr.StatusCheckRollup)
		baseStatus := normalizePRStatus(pr.State, pr.MergedAt, pr.IsDraft)
		reviewApproved, reviewRequired, reviewKnown := reviewProgressFromDecision(pr.ReviewDecision, strings.EqualFold(strings.TrimSpace(pr.ReviewDecision), "approved"))
		base := strings.TrimSpace(pr.BaseRefName)
		if info, ok := requiredByBase[base]; ok && info.known {
			reviewRequired = info.count
			reviewKnown = true
		}
		reviewSatisfied := hasSufficientApprovals(reviewApproved, reviewRequired, reviewKnown, pr.ReviewDecision, strings.EqualFold(strings.TrimSpace(pr.ReviewDecision), "approved"))
		status := computePRStatus(pr.State, pr.MergedAt, pr.IsDraft, pr.MergeStateStatus, reviewSatisfied, ciState, 0, false)
		prList = append(prList, PRListData{
			Number:              pr.Number,
			URL:                 strings.TrimSpace(pr.URL),
			Branch:              branch,
			Title:               strings.TrimSpace(pr.Title),
			Status:              status,
			ReviewDecision:      strings.TrimSpace(pr.ReviewDecision),
			Approved:            strings.EqualFold(strings.TrimSpace(pr.ReviewDecision), "approved"),
			ReviewApproved:      reviewApproved,
			ReviewRequired:      reviewRequired,
			ReviewKnown:         reviewKnown,
			CIState:             ciState,
			CICompleted:         ciDone,
			CITotal:             ciTotal,
			CIFailingNames:      failingNames,
			UnresolvedComments:  0,
			ResolvedComments:    0,
			CommentThreadsTotal: 0,
			UpdatedAt:           updatedAt,
			CommentsKnown:       false,
			MergeStateStatus:    strings.TrimSpace(pr.MergeStateStatus),
			BaseStatus:          baseStatus,
		})
	}
	sort.SliceStable(prList, func(i, j int) bool {
		iBucket := prStatusSortBucket(prList[i].Status)
		jBucket := prStatusSortBucket(prList[j].Status)
		if iBucket != jBucket {
			return iBucket < jBucket
		}
		if !prList[i].UpdatedAt.Equal(prList[j].UpdatedAt) {
			return prList[i].UpdatedAt.After(prList[j].UpdatedAt)
		}
		return prList[i].Number > prList[j].Number
	})

	if owner == "" || name == "" {
		return prList, nil
	}

	type enrichResult struct {
		index         int
		count         int
		countKnown    bool
		unresolved    int
		resolved      int
		total         int
		commentsKnown bool
		ok            bool
	}
	results := make(chan enrichResult, len(prList))
	sem := make(chan struct{}, maxPREnrichmentParallel)
	var wg sync.WaitGroup
	for i := range prList {
		if prList[i].Number <= 0 {
			continue
		}
		wg.Add(1)
		go func(idx int, number int, baseStatus string, reviewRequired int) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			count := 0
			countKnown := false
			if reviewRequired > 0 {
				countResult, countErr := approvedReviewsCount(ghPath, repoRoot, owner, name, number)
				if countErr != nil {
					results <- enrichResult{index: idx, ok: false}
					return
				}
				count = countResult
				countKnown = true
			}
			unresolved := 0
			resolved := 0
			total := 0
			commentsKnown := false
			if baseStatus == "open" || baseStatus == "draft" {
				if counts, unresolvedErr := reviewThreadCountsForPR(ghPath, repoRoot, owner, name, number); unresolvedErr == nil {
					unresolved = counts.Unresolved
					resolved = counts.Resolved
					total = counts.Total
					commentsKnown = true
				}
			}
			results <- enrichResult{index: idx, count: count, countKnown: countKnown, unresolved: unresolved, resolved: resolved, total: total, commentsKnown: commentsKnown, ok: true}
		}(i, prList[i].Number, strings.TrimSpace(strings.ToLower(prList[i].BaseStatus)), prList[i].ReviewRequired)
	}
	go func() {
		wg.Wait()
		close(results)
	}()
	for res := range results {
		if !res.ok {
			continue
		}
		if res.countKnown {
			prList[res.index].ReviewApproved = res.count
			prList[res.index].ReviewKnown = true
			if prList[res.index].ReviewApproved > prList[res.index].ReviewRequired {
				prList[res.index].ReviewRequired = prList[res.index].ReviewApproved
			}
		}
		prList[res.index].CommentsKnown = res.commentsKnown
		prList[res.index].UnresolvedComments = res.unresolved
		prList[res.index].ResolvedComments = res.resolved
		prList[res.index].CommentThreadsTotal = res.total
		reviewSatisfied := hasSufficientApprovals(
			prList[res.index].ReviewApproved,
			prList[res.index].ReviewRequired,
			prList[res.index].ReviewKnown,
			prList[res.index].ReviewDecision,
			prList[res.index].Approved,
		)
		prList[res.index].Status = computePRStatus(
			prList[res.index].BaseStatus,
			"",
			prList[res.index].BaseStatus == "draft",
			prList[res.index].MergeStateStatus,
			reviewSatisfied,
			prList[res.index].CIState,
			prList[res.index].UnresolvedComments,
			prList[res.index].CommentsKnown,
		)
	}
	return prList, nil
}

func (m *GHManager) fetchPRDataForBranches(repoRoot string, branches []string) (map[string]PRData, error) {
	if len(branches) == 0 {
		return map[string]PRData{}, nil
	}
	if _, err := exec.LookPath("gh"); err != nil {
		return nil, err
	}
	ghPath, err := exec.LookPath("gh")
	if err != nil {
		return nil, err
	}
	owner, name, err := resolveGitHubRepo(repoRoot)
	if err != nil {
		owner, name = "", ""
	}
	type branchResult struct {
		branch string
		data   PRData
		found  bool
		err    error
	}
	results := make(chan branchResult, len(branches))
	sem := make(chan struct{}, maxBranchFetchParallel)
	var wg sync.WaitGroup
	for _, branch := range branches {
		b := strings.TrimSpace(branch)
		if b == "" || b == "detached" {
			continue
		}
		wg.Add(1)
		go func(branchName string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			data, found, fetchErr := ghPRDataForBranch(ghPath, repoRoot, owner, name, branchName)
			results <- branchResult{
				branch: branchName,
				data:   data,
				found:  found,
				err:    fetchErr,
			}
		}(b)
	}
	go func() {
		wg.Wait()
		close(results)
	}()
	out := make(map[string]PRData, len(branches))
	var firstErr error
	for res := range results {
		if res.err != nil && firstErr == nil {
			firstErr = res.err
		}
		if res.found {
			out[res.branch] = res.data
		}
	}
	return out, firstErr
}

func ghPRList(ghPath string, repoRoot string, fields string, limit string, timeout time.Duration) ([]ghPR, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	listLimit := strings.TrimSpace(limit)
	if listLimit == "" {
		listLimit = defaultPRListFetchLimit
	}
	cmd := exec.CommandContext(
		ctx,
		ghPath,
		"pr",
		"list",
		"--state", "all",
		"--author", "@me",
		"--json", fields,
		"--limit", listLimit,
	)
	cmd.Dir = repoRoot
	out, err := cmd.CombinedOutput()
	if err != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return nil, fmt.Errorf("gh pr list timed out after %s", timeout.Round(time.Second))
		}
		msg := strings.TrimSpace(string(out))
		if msg == "" {
			return nil, err
		}
		return nil, fmt.Errorf("%w: %s", err, msg)
	}
	var prs []ghPR
	if err := json.Unmarshal(out, &prs); err != nil {
		return nil, err
	}
	return prs, nil
}

func ghPRDataForBranch(ghPath string, repoRoot string, owner string, name string, branch string) (PRData, bool, error) {
	pr, found, err := ghPRViewByBranch(ghPath, repoRoot, branch, fullPRListFields, ghPRHeadFullTimeout)
	if err != nil {
		pr, found, err = ghPRViewByBranch(ghPath, repoRoot, branch, fallbackPRListFields, ghPRHeadFallbackTimeout)
		if err != nil {
			return PRData{}, false, err
		}
	}
	if !found {
		return PRData{}, false, nil
	}
	ciState, ciDone, ciTotal, failingNames := summarizeCI(pr.StatusCheckRollup)
	reviewApproved, reviewRequired, reviewKnown := reviewProgressForPR(ghPath, repoRoot, owner, name, pr.Number, pr.BaseRefName, pr.ReviewDecision, strings.EqualFold(strings.TrimSpace(pr.ReviewDecision), "approved"))
	reviewSatisfied := hasSufficientApprovals(reviewApproved, reviewRequired, reviewKnown, pr.ReviewDecision, strings.EqualFold(strings.TrimSpace(pr.ReviewDecision), "approved"))
	data := PRData{
		Number:         pr.Number,
		URL:            strings.TrimSpace(pr.URL),
		Branch:         strings.TrimSpace(pr.HeadRefName),
		Status:         "-",
		ReviewDecision: strings.TrimSpace(pr.ReviewDecision),
		Approved:       strings.EqualFold(strings.TrimSpace(pr.ReviewDecision), "approved"),
		ReviewApproved: reviewApproved,
		ReviewRequired: reviewRequired,
		ReviewKnown:    reviewKnown,
		CIState:        ciState,
		CICompleted:    ciDone,
		CITotal:        ciTotal,
		CIFailingNames: failingNames,
	}
	baseStatus := normalizePRStatus(pr.State, pr.MergedAt, pr.IsDraft)
	if owner != "" && name != "" && pr.Number > 0 && (baseStatus == "open" || baseStatus == "draft") {
		if counts, uerr := reviewThreadCountsForPR(ghPath, repoRoot, owner, name, pr.Number); uerr == nil {
			data.UnresolvedComments = counts.Unresolved
			data.ResolvedComments = counts.Resolved
			data.CommentThreadsTotal = counts.Total
			data.CommentsKnown = true
		}
	}
	data.Status = computePRStatus(pr.State, pr.MergedAt, pr.IsDraft, pr.MergeStateStatus, reviewSatisfied, ciState, data.UnresolvedComments, data.CommentsKnown)
	data.BaseStatus = baseStatus
	if strings.TrimSpace(data.Branch) == "" {
		data.Branch = branch
	}
	return data, true, nil
}

func ghPRViewByBranch(ghPath string, repoRoot string, branch string, fields string, timeout time.Duration) (ghPR, bool, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(
		ctx,
		ghPath,
		"pr",
		"view",
		branch,
		"--json", fields,
	)
	cmd.Dir = repoRoot
	out, err := cmd.CombinedOutput()
	if err != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return ghPR{}, false, fmt.Errorf("gh pr view timed out after %s", timeout.Round(time.Second))
		}
		msg := strings.TrimSpace(string(out))
		if strings.Contains(strings.ToLower(msg), "no pull requests found for branch") {
			return ghPR{}, false, nil
		}
		if strings.Contains(strings.ToLower(msg), "not found") && strings.Contains(strings.ToLower(msg), "pull request") {
			return ghPR{}, false, nil
		}
		if msg == "" {
			return ghPR{}, false, err
		}
		return ghPR{}, false, fmt.Errorf("%w: %s", err, msg)
	}
	var pr ghPR
	if err := json.Unmarshal(out, &pr); err != nil {
		return ghPR{}, false, err
	}
	return pr, true, nil
}

func reviewProgressForPR(ghPath string, repoRoot string, owner string, name string, number int, baseRefName string, reviewDecision string, approved bool) (int, int, bool) {
	requiredCount := 0
	requiredKnown := false
	baseRefName = strings.TrimSpace(baseRefName)
	if owner != "" && name != "" && baseRefName != "" {
		if count, known, err := requiredApprovalsForBaseBranch(ghPath, repoRoot, owner, name, baseRefName); err == nil && known {
			requiredCount = count
			requiredKnown = true
		}
	}

	approvedCount := 0
	approvedKnown := false
	if owner != "" && name != "" && number > 0 {
		if count, err := approvedReviewsCount(ghPath, repoRoot, owner, name, number); err == nil {
			approvedCount = count
			approvedKnown = true
		}
	}

	decision := strings.ToUpper(strings.TrimSpace(reviewDecision))
	if !requiredKnown {
		switch decision {
		case "REVIEW_REQUIRED", "APPROVED":
			requiredCount = 1
			requiredKnown = true
		}
	}
	if !approvedKnown {
		switch decision {
		case "APPROVED":
			approvedCount = 1
			approvedKnown = true
		case "REVIEW_REQUIRED":
			approvedCount = 0
			approvedKnown = true
		default:
			if approved {
				approvedCount = 1
				approvedKnown = true
			}
		}
	}
	return approvedCount, requiredCount, approvedKnown || requiredKnown
}

func reviewProgressFromDecision(reviewDecision string, approved bool) (int, int, bool) {
	decision := strings.ToUpper(strings.TrimSpace(reviewDecision))
	switch decision {
	case "APPROVED":
		return 1, 1, true
	case "REVIEW_REQUIRED", "CHANGES_REQUESTED":
		return 0, 1, true
	default:
		if approved {
			return 1, 1, true
		}
		return 0, 0, false
	}
}

func requiredApprovalsForBaseBranch(ghPath string, repoRoot string, owner string, name string, baseRefName string) (int, bool, error) {
	endpoint := fmt.Sprintf("repos/%s/%s/branches/%s/protection", owner, name, url.PathEscape(baseRefName))
	ctx, cancel := context.WithTimeout(context.Background(), ghProtectionTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, ghPath, "api", endpoint)
	cmd.Dir = repoRoot
	out, err := cmd.CombinedOutput()
	if err != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return 0, false, fmt.Errorf("gh api protection timed out after %s", ghProtectionTimeout.Round(time.Second))
		}
		msg := strings.ToLower(strings.TrimSpace(string(out)))
		if strings.Contains(msg, "branch not protected") || strings.Contains(msg, "404") {
			return 0, true, nil
		}
		return 0, false, err
	}
	var resp ghBranchProtectionResp
	if err := json.Unmarshal(out, &resp); err != nil {
		return 0, false, err
	}
	if resp.RequiredPullRequestReviews == nil {
		return 0, true, nil
	}
	return resp.RequiredPullRequestReviews.RequiredApprovingReviewCount, true, nil
}

func approvedReviewsCount(ghPath string, repoRoot string, owner string, name string, number int) (int, error) {
	endpoint := fmt.Sprintf("repos/%s/%s/pulls/%d/reviews?per_page=100", owner, name, number)
	ctx, cancel := context.WithTimeout(context.Background(), ghReviewCountTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, ghPath, "api", endpoint)
	cmd.Dir = repoRoot
	out, err := cmd.CombinedOutput()
	if err != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return 0, fmt.Errorf("gh api reviews timed out after %s", ghReviewCountTimeout.Round(time.Second))
		}
		return 0, err
	}
	var reviews []ghPullReview
	if err := json.Unmarshal(out, &reviews); err != nil {
		return 0, err
	}
	latestByUser := make(map[string]string, len(reviews))
	for _, r := range reviews {
		login := strings.TrimSpace(strings.ToLower(r.User.Login))
		if login == "" {
			continue
		}
		latestByUser[login] = strings.TrimSpace(strings.ToUpper(r.State))
	}
	count := 0
	for _, state := range latestByUser {
		if state == "APPROVED" {
			count++
		}
	}
	return count, nil
}

func fetchRequiredApprovalsByBaseRefs(ghPath string, repoRoot string, owner string, name string, prs []ghPR) map[string]requiredApprovalsInfo {
	out := make(map[string]requiredApprovalsInfo)
	seen := make(map[string]bool)
	baseRefs := make([]string, 0, len(prs))
	for _, pr := range prs {
		base := strings.TrimSpace(pr.BaseRefName)
		if base == "" || seen[base] {
			continue
		}
		seen[base] = true
		baseRefs = append(baseRefs, base)
	}
	type baseResult struct {
		base  string
		count int
		known bool
	}
	results := make(chan baseResult, len(baseRefs))
	sem := make(chan struct{}, maxPREnrichmentParallel)
	var wg sync.WaitGroup
	for _, base := range baseRefs {
		wg.Add(1)
		go func(baseRef string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			count, known, err := requiredApprovalsForBaseBranch(ghPath, repoRoot, owner, name, baseRef)
			if err != nil {
				results <- baseResult{base: baseRef, count: 0, known: false}
				return
			}
			results <- baseResult{base: baseRef, count: count, known: known}
		}(base)
	}
	go func() {
		wg.Wait()
		close(results)
	}()
	for res := range results {
		out[res.base] = requiredApprovalsInfo{count: res.count, known: res.known}
	}
	return out
}

func parseGitHubTime(value string) time.Time {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}
	}
	t, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return time.Time{}
	}
	return t
}

func normalizePRStatus(state string, mergedAt string, isDraft bool) string {
	if strings.TrimSpace(mergedAt) != "" {
		return "merged"
	}
	switch strings.ToUpper(strings.TrimSpace(state)) {
	case "CLOSED":
		return "closed"
	case "MERGED":
		return "merged"
	case "DRAFT":
		return "draft"
	case "OPEN":
		if isDraft {
			return "draft"
		}
		return "open"
	default:
		return "-"
	}
}

func prStatusSortBucket(status string) int {
	switch strings.TrimSpace(strings.ToLower(status)) {
	case "merged", "closed":
		return 1
	default:
		return 0
	}
}

func hasSufficientApprovals(reviewApproved int, reviewRequired int, reviewKnown bool, reviewDecision string, approved bool) bool {
	if reviewRequired > 0 {
		return reviewApproved >= reviewRequired
	}
	if reviewKnown {
		return reviewApproved > 0
	}
	decision := strings.ToUpper(strings.TrimSpace(reviewDecision))
	if decision == "APPROVED" {
		return true
	}
	return approved
}

func hasConflictPRStatus(mergeStateStatus string) bool {
	return strings.ToUpper(strings.TrimSpace(mergeStateStatus)) == "DIRTY"
}

func computePRStatus(state string, mergedAt string, isDraft bool, mergeStateStatus string, reviewSatisfied bool, ciState PRCIState, unresolvedComments int, commentsKnown bool) string {
	base := normalizePRStatus(state, mergedAt, isDraft)
	if base == "merged" {
		return "merged"
	}
	if base == "closed" {
		return "closed"
	}
	if hasConflictPRStatus(mergeStateStatus) {
		return "conflict"
	}
	ciPassed := ciState == PRCISuccess
	commentsResolved := commentsKnown && unresolvedComments <= 0
	if reviewSatisfied && ciPassed && commentsResolved {
		return "can-merge"
	}
	if !reviewSatisfied {
		return "awaiting-review"
	}
	if !ciPassed {
		return "awaiting-ci"
	}
	if commentsKnown && unresolvedComments > 0 {
		return "awaiting-comments"
	}
	if base == "draft" {
		return "draft"
	}
	if base == "open" {
		return "open"
	}
	return base
}

func summarizeCI(checks []ghCheck) (PRCIState, int, int, string) {
	if len(checks) == 0 {
		return PRCINone, 0, 0, ""
	}
	total := 0
	completed := 0
	inProgress := false
	failed := false
	failingNamesSet := map[string]bool{}
	failingNames := make([]string, 0, len(checks))
	for _, c := range checks {
		status := strings.ToUpper(strings.TrimSpace(c.Status))
		conclusion := strings.ToUpper(strings.TrimSpace(c.Conclusion))
		if status == "" && conclusion == "" {
			continue
		}
		total++
		if conclusion != "" {
			completed++
			switch conclusion {
			case "SUCCESS", "SKIPPED", "NEUTRAL":
			default:
				failed = true
				name := strings.TrimSpace(c.Name)
				if name == "" {
					name = strings.TrimSpace(c.Context)
				}
				if name != "" && !failingNamesSet[name] {
					failingNamesSet[name] = true
					failingNames = append(failingNames, name)
				}
			}
		}
		if status != "" && status != "COMPLETED" {
			inProgress = true
		}
		if conclusion == "" {
			inProgress = true
		}
	}
	if total == 0 {
		return PRCINone, 0, 0, ""
	}
	sort.Strings(failingNames)
	failingLabel := strings.Join(failingNames, ",")
	if failed {
		return PRCIFail, completed, total, failingLabel
	}
	if inProgress || completed < total {
		return PRCIInProgress, completed, total, ""
	}
	return PRCISuccess, completed, total, ""
}

type reviewThreadCounts struct {
	Resolved   int
	Unresolved int
	Total      int
}

func reviewThreadCountsForPR(ghPath string, repoRoot string, owner string, name string, number int) (reviewThreadCounts, error) {
	if owner == "" || name == "" || number <= 0 {
		return reviewThreadCounts{}, errors.New("repo/number required")
	}
	query := `query($owner:String!,$name:String!,$number:Int!,$after:String){repository(owner:$owner,name:$name){pullRequest(number:$number){reviewThreads(first:100,after:$after){totalCount pageInfo{hasNextPage endCursor} nodes{isResolved}}}}}`
	ctx, cancel := context.WithTimeout(context.Background(), ghUnresolvedPRTimeout)
	defer cancel()
	after := ""
	total := 0
	unresolved := 0
	seenTotal := false
	for {
		args := []string{"api", "graphql", "-f", "query=" + query, "-F", "owner=" + owner, "-F", "name=" + name, "-F", fmt.Sprintf("number=%d", number)}
		if after != "" {
			args = append(args, "-F", "after="+after)
		}
		cmd := exec.CommandContext(ctx, ghPath, args...)
		cmd.Dir = repoRoot
		out, err := cmd.Output()
		if err != nil {
			if errors.Is(ctx.Err(), context.DeadlineExceeded) {
				return reviewThreadCounts{}, fmt.Errorf("gh api graphql timed out after %s", ghUnresolvedPRTimeout.Round(time.Second))
			}
			return reviewThreadCounts{}, err
		}
		var resp ghReviewThreadsResp
		if err := json.Unmarshal(out, &resp); err != nil {
			return reviewThreadCounts{}, err
		}
		rt := resp.Data.Repository.PullRequest.ReviewThreads
		if !seenTotal {
			total = rt.TotalCount
			seenTotal = true
		}
		for _, t := range rt.Nodes {
			if !t.IsResolved {
				unresolved++
			}
		}
		if !rt.PageInfo.HasNextPage || strings.TrimSpace(rt.PageInfo.EndCursor) == "" {
			break
		}
		after = rt.PageInfo.EndCursor
	}
	if unresolved > total {
		unresolved = total
	}
	resolved := total - unresolved
	if resolved < 0 {
		resolved = 0
	}
	return reviewThreadCounts{
		Resolved:   resolved,
		Unresolved: unresolved,
		Total:      total,
	}, nil
}

func resolveGitHubRepo(repoRoot string) (string, string, error) {
	gitPath, err := requireGitPath()
	if err != nil {
		return "", "", err
	}
	remote, err := gitOutputInDir(repoRoot, gitPath, "remote", "get-url", "origin")
	if err != nil {
		return "", "", err
	}
	remote = strings.TrimSpace(remote)
	if remote == "" {
		return "", "", errors.New("origin remote missing")
	}
	if strings.HasPrefix(remote, "git@github.com:") {
		path := strings.TrimPrefix(remote, "git@github.com:")
		return splitOwnerRepo(path)
	}
	if strings.HasPrefix(remote, "https://github.com/") {
		path := strings.TrimPrefix(remote, "https://github.com/")
		return splitOwnerRepo(path)
	}
	if strings.HasPrefix(remote, "http://github.com/") {
		path := strings.TrimPrefix(remote, "http://github.com/")
		return splitOwnerRepo(path)
	}
	return "", "", errors.New("non-github origin")
}

func splitOwnerRepo(path string) (string, string, error) {
	path = strings.TrimSpace(path)
	path = strings.TrimSuffix(path, ".git")
	path = strings.Trim(path, "/")
	parts := strings.Split(path, "/")
	if len(parts) < 2 {
		return "", "", errors.New("invalid github repo path")
	}
	owner := parts[0]
	repo := parts[1]
	if owner == "" || repo == "" {
		return "", "", errors.New("invalid github repo path")
	}
	return owner, filepath.Base(repo), nil
}
