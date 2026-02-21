package main

import (
	"sort"
	"strings"
)

const (
	completionTier0Limit  = 12
	completionTier1Local  = 40
	completionTier1Remote = 60
	completionTier2Limit  = 80
)

func branchExistsLocalOrRemote(repoRoot string, gitPath string, branch string) (bool, error) {
	branch = strings.TrimSpace(branch)
	if branch == "" {
		return false, nil
	}
	if localBranchExists(repoRoot, gitPath, branch) {
		return true, nil
	}
	if _, err := gitOutputInDir(repoRoot, gitPath, "show-ref", "--verify", "refs/remotes/"+branch); err == nil {
		return true, nil
	}
	remotes, err := listRemoteTrackingBranchNames(repoRoot, gitPath, 0)
	if err != nil {
		return false, err
	}
	for _, remoteBranch := range remotes {
		if remoteBranch == branch {
			return true, nil
		}
	}
	return false, nil
}

func completeBranchSuggestions(toComplete string) []string {
	gitPath, repoRoot, err := requireGitContext("")
	if err != nil {
		return []string{}
	}

	prefix := strings.TrimSpace(toComplete)
	seen := map[string]bool{}
	out := make([]string, 0, completionTier0Limit+completionTier1Local+completionTier1Remote)

	appendMatching := func(values []string) {
		for _, value := range values {
			v := strings.TrimSpace(value)
			if v == "" || v == "detached" || seen[v] {
				continue
			}
			if !matchesCompletionPrefix(v, prefix) {
				continue
			}
			seen[v] = true
			out = append(out, v)
		}
	}

	if recent, err := readRecentBranches(repoRoot, completionTier0Limit); err == nil {
		appendMatching(recent)
	}

	if local, err := listLocalBranchNames(repoRoot, gitPath, completionTier1Local); err == nil {
		appendMatching(local)
	}

	if remote, err := listRemoteTrackingBranchNames(repoRoot, gitPath, completionTier1Remote); err == nil {
		appendMatching(remote)
	}

	if n := len(prefix); n >= 3 && n <= 4 {
		if remotePrefix, err := searchRemoteBranchesByPrefix(repoRoot, gitPath, prefix, completionTier2Limit); err == nil {
			appendMatching(remotePrefix)
		}
	}

	return out
}

func listLocalBranchNames(repoRoot string, gitPath string, limit int) ([]string, error) {
	args := []string{
		"for-each-ref",
		"--sort=-committerdate",
		"--format=%(refname:short)",
		"refs/heads",
	}
	if limit > 0 {
		args = append(args, "--count", itoa(limit))
	}
	out, err := commandOutputInDir(repoRoot, gitPath, args...)
	if err != nil {
		return nil, err
	}
	return parseBranchLines(string(out)), nil
}

func listRemoteTrackingBranchNames(repoRoot string, gitPath string, limit int) ([]string, error) {
	args := []string{
		"for-each-ref",
		"--sort=-committerdate",
		"--format=%(refname:short)",
		"refs/remotes",
	}
	if limit > 0 {
		args = append(args, "--count", itoa(limit))
	}
	out, err := commandOutputInDir(repoRoot, gitPath, args...)
	if err != nil {
		return nil, err
	}
	refs := parseBranchLines(string(out))
	branches := make([]string, 0, len(refs))
	seen := map[string]bool{}
	for _, ref := range refs {
		name := shortBranch(ref)
		if name == "" || name == "detached" || strings.EqualFold(name, "head") || seen[name] {
			continue
		}
		seen[name] = true
		branches = append(branches, name)
	}
	return branches, nil
}

func searchRemoteBranchesByPrefix(repoRoot string, gitPath string, prefix string, limit int) ([]string, error) {
	prefix = strings.ToLower(strings.TrimSpace(prefix))
	if prefix == "" {
		return []string{}, nil
	}
	remote, err := listRemoteTrackingBranchNames(repoRoot, gitPath, 0)
	if err != nil {
		return nil, err
	}
	matches := make([]string, 0, min(limit, len(remote)))
	for _, branch := range remote {
		if !strings.HasPrefix(strings.ToLower(branch), prefix) {
			continue
		}
		matches = append(matches, branch)
		if limit > 0 && len(matches) >= limit {
			break
		}
	}
	sort.Strings(matches)
	return matches, nil
}

func parseBranchLines(output string) []string {
	lines := strings.Split(output, "\n")
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		value := strings.TrimSpace(line)
		if value == "" {
			continue
		}
		out = append(out, value)
	}
	return out
}

func matchesCompletionPrefix(value string, prefix string) bool {
	if strings.TrimSpace(prefix) == "" {
		return true
	}
	valueLower := strings.ToLower(strings.TrimSpace(value))
	prefixLower := strings.ToLower(strings.TrimSpace(prefix))
	return strings.HasPrefix(valueLower, prefixLower)
}

func itoa(value int) string {
	if value == 0 {
		return "0"
	}
	neg := value < 0
	if neg {
		value = -value
	}
	buf := [20]byte{}
	i := len(buf)
	for value > 0 {
		i--
		buf[i] = byte('0' + (value % 10))
		value /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
