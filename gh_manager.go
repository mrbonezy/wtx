package main

import (
	"encoding/json"
	"errors"
	"fmt"
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
)

type PRData struct {
	Number             int
	URL                string
	Branch             string
	Status             string
	ReviewDecision     string
	Approved           bool
	UnresolvedComments int
	CIState            PRCIState
	CICompleted        int
	CITotal            int
}

type PRListData struct {
	Number         int
	URL            string
	Branch         string
	Title          string
	Status         string
	ReviewDecision string
	Approved       bool
	CIState        PRCIState
	CICompleted    int
	CITotal        int
	UpdatedAt      time.Time
}

type GHManager struct {
	mu    sync.Mutex
	cache map[string]ghRepoCache
	ttl   time.Duration
}

type ghRepoCache struct {
	fetchedAt time.Time
	prs       map[string]PRData
	prList    []PRListData
}

type ghPR struct {
	Number            int       `json:"number"`
	URL               string    `json:"url"`
	HeadRefName       string    `json:"headRefName"`
	Title             string    `json:"title"`
	IsDraft           bool      `json:"isDraft"`
	State             string    `json:"state"`
	UpdatedAt         string    `json:"updatedAt"`
	MergedAt          string    `json:"mergedAt"`
	ReviewDecision    string    `json:"reviewDecision"`
	StatusCheckRollup []ghCheck `json:"statusCheckRollup"`
}

type ghCheck struct {
	Conclusion string `json:"conclusion"`
	Status     string `json:"status"`
}

type ghReviewThreadsResp struct {
	Data struct {
		Repository struct {
			PullRequest struct {
				ReviewThreads struct {
					Nodes []struct {
						IsResolved bool `json:"isResolved"`
					} `json:"nodes"`
				} `json:"reviewThreads"`
			} `json:"pullRequest"`
		} `json:"repository"`
	} `json:"data"`
}

func NewGHManager() *GHManager {
	return &GHManager{
		cache: make(map[string]ghRepoCache),
		ttl:   20 * time.Second,
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
	cached, fetchErr := m.ensureRepoCache(repoRoot, force)
	out := make([]PRListData, len(cached.prList))
	copy(out, cached.prList)
	return out, fetchErr
}

func (m *GHManager) prDataByBranch(repoRoot string, branches []string, force bool) (map[string]PRData, error) {
	repoRoot = strings.TrimSpace(repoRoot)
	if repoRoot == "" || len(branches) == 0 {
		return map[string]PRData{}, nil
	}
	cached, fetchErr := m.ensureRepoCache(repoRoot, force)
	out := make(map[string]PRData, len(branches))
	for _, b := range branches {
		if d, ok := cached.prs[b]; ok {
			out[b] = d
		}
	}
	return out, fetchErr
}

func (m *GHManager) ensureRepoCache(repoRoot string, force bool) (ghRepoCache, error) {
	m.mu.Lock()
	cached, ok := m.cache[repoRoot]
	fresh := !force && ok && time.Since(cached.fetchedAt) < m.ttl
	m.mu.Unlock()

	var fetchErr error
	if !fresh {
		prsByBranch, prs, err := m.fetchRepoPRData(repoRoot)
		if err == nil {
			m.mu.Lock()
			m.cache[repoRoot] = ghRepoCache{fetchedAt: time.Now(), prs: prsByBranch, prList: prs}
			cached = m.cache[repoRoot]
			m.mu.Unlock()
		} else {
			fetchErr = err
		}
	}
	return cached, fetchErr
}

func (m *GHManager) fetchRepoPRData(repoRoot string) (map[string]PRData, []PRListData, error) {
	if _, err := exec.LookPath("gh"); err != nil {
		return nil, nil, err
	}
	ghPath, err := exec.LookPath("gh")
	if err != nil {
		return nil, nil, err
	}
	cmd := exec.Command(ghPath, "pr", "list", "--state", "all", "--json", "number,url,headRefName,title,isDraft,state,updatedAt,mergedAt,reviewDecision,statusCheckRollup", "--limit", "200")
	cmd.Dir = repoRoot
	out, err := cmd.CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if msg == "" {
			return nil, nil, err
		}
		return nil, nil, fmt.Errorf("%w: %s", err, msg)
	}
	var prs []ghPR
	if err := json.Unmarshal(out, &prs); err != nil {
		return nil, nil, err
	}
	owner, name, err := resolveGitHubRepo(repoRoot)
	if err != nil {
		owner, name = "", ""
	}
	result := make(map[string]PRData, len(prs))
	latestUpdated := make(map[string]time.Time, len(prs))
	prList := make([]PRListData, 0, len(prs))
	for _, pr := range prs {
		branch := strings.TrimSpace(pr.HeadRefName)
		if branch == "" {
			continue
		}
		updatedAt := parseGitHubTime(pr.UpdatedAt)
		ciState, ciDone, ciTotal := summarizeCI(pr.StatusCheckRollup)
		status := normalizePRStatus(pr.State, pr.MergedAt, pr.IsDraft)
		data := PRData{
			Number:         pr.Number,
			URL:            strings.TrimSpace(pr.URL),
			Branch:         branch,
			Status:         status,
			ReviewDecision: strings.TrimSpace(pr.ReviewDecision),
			Approved:       strings.EqualFold(strings.TrimSpace(pr.ReviewDecision), "approved"),
			CIState:        ciState,
			CICompleted:    ciDone,
			CITotal:        ciTotal,
		}
		if (status == "open" || status == "draft") && owner != "" && name != "" && pr.Number > 0 {
			if unresolved, uerr := unresolvedCommentsForPR(ghPath, repoRoot, owner, name, pr.Number); uerr == nil {
				data.UnresolvedComments = unresolved
			}
		}
		if t, ok := latestUpdated[branch]; !ok || updatedAt.After(t) {
			latestUpdated[branch] = updatedAt
			result[branch] = data
		}
		prList = append(prList, PRListData{
			Number:         pr.Number,
			URL:            strings.TrimSpace(pr.URL),
			Branch:         branch,
			Title:          strings.TrimSpace(pr.Title),
			Status:         status,
			ReviewDecision: strings.TrimSpace(pr.ReviewDecision),
			Approved:       strings.EqualFold(strings.TrimSpace(pr.ReviewDecision), "approved"),
			CIState:        ciState,
			CICompleted:    ciDone,
			CITotal:        ciTotal,
			UpdatedAt:      updatedAt,
		})
	}
	sort.SliceStable(prList, func(i, j int) bool {
		return prList[i].UpdatedAt.After(prList[j].UpdatedAt)
	})
	return result, prList, nil
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
	if isDraft {
		return "draft"
	}
	switch strings.ToUpper(strings.TrimSpace(state)) {
	case "OPEN":
		return "open"
	case "CLOSED":
		return "closed"
	case "MERGED":
		return "merged"
	default:
		return "-"
	}
}

func summarizeCI(checks []ghCheck) (PRCIState, int, int) {
	if len(checks) == 0 {
		return PRCINone, 0, 0
	}
	total := 0
	completed := 0
	inProgress := false
	failed := false
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
		return PRCINone, 0, 0
	}
	if failed {
		return PRCIFail, completed, total
	}
	if inProgress || completed < total {
		return PRCIInProgress, completed, total
	}
	return PRCISuccess, completed, total
}

func unresolvedCommentsForPR(ghPath string, repoRoot string, owner string, name string, number int) (int, error) {
	if owner == "" || name == "" || number <= 0 {
		return 0, errors.New("repo/number required")
	}
	query := `query($owner:String!,$name:String!,$number:Int!){repository(owner:$owner,name:$name){pullRequest(number:$number){reviewThreads(first:100){nodes{isResolved}}}}}`
	cmd := exec.Command(ghPath, "api", "graphql", "-f", "query="+query, "-F", "owner="+owner, "-F", "name="+name, "-F", fmt.Sprintf("number=%d", number))
	cmd.Dir = repoRoot
	out, err := cmd.Output()
	if err != nil {
		return 0, err
	}
	var resp ghReviewThreadsResp
	if err := json.Unmarshal(out, &resp); err != nil {
		return 0, err
	}
	count := 0
	for _, t := range resp.Data.Repository.PullRequest.ReviewThreads.Nodes {
		if !t.IsResolved {
			count++
		}
	}
	return count, nil
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
