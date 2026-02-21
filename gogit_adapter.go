package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	git "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/plumbing/transport"
	gitssh "github.com/go-git/go-git/v5/plumbing/transport/ssh"
	sshconfig "github.com/kevinburke/ssh_config"
)

var sshConfigGet = func(alias, key string) string {
	return sshconfig.Get(alias, key)
}

var sshConfigGetAll = func(alias, key string) []string {
	return sshconfig.GetAll(alias, key)
}

func isGitBinary(path string) bool {
	name := strings.ToLower(strings.TrimSpace(filepath.Base(path)))
	return name == "git" || name == "git.exe"
}

func gitCommandOutputInDir(dir string, args ...string) (string, bool, error) {
	if len(args) == 0 {
		return "", false, nil
	}
	if isLinkedWorktreeDir(dir) {
		// go-git linked-worktree support is incomplete for command emulation;
		// use the real git binary in those directories.
		return "", false, nil
	}

	switch args[0] {
	case "worktree":
		// go-git does not support full linked-worktree lifecycle parity.
		return "", false, nil
	case "rev-parse":
		return gitRevParse(dir, args[1:])
	case "branch":
		return gitBranch(dir, args[1:])
	case "show-ref":
		return gitShowRef(dir, args[1:])
	case "remote":
		return gitRemote(dir, args[1:])
	case "for-each-ref":
		return gitForEachRef(dir, args[1:])
	case "status":
		return gitStatusPorcelain(dir, args[1:])
	case "fetch":
		return gitFetch(dir, args[1:])
	case "checkout":
		return gitCheckout(dir, args[1:])
	default:
		return "", false, nil
	}
}

func isLinkedWorktreeDir(dir string) bool {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return false
	}
	dotGit := filepath.Join(dir, ".git")
	info, err := os.Stat(dotGit)
	if err != nil || info.IsDir() {
		return false
	}
	data, err := os.ReadFile(dotGit)
	if err != nil {
		return false
	}
	return strings.HasPrefix(strings.TrimSpace(string(data)), "gitdir:")
}

func openRepo(dir string) (*git.Repository, string, error) {
	repoRoot, err := repoRootForDir(dir, "")
	if err != nil {
		return nil, "", err
	}
	repo, err := git.PlainOpenWithOptions(repoRoot, &git.PlainOpenOptions{DetectDotGit: true})
	if err != nil {
		return nil, "", err
	}
	return repo, repoRoot, nil
}

func gitRevParse(dir string, args []string) (string, bool, error) {
	if len(args) == 1 && args[0] == "--show-toplevel" {
		root, err := repoRootForDir(dir, "")
		return root + "\n", true, err
	}
	if len(args) == 2 && args[0] == "--abbrev-ref" && args[1] == "HEAD" {
		repo, _, err := openRepo(dir)
		if err != nil {
			return "", true, err
		}
		head, err := repo.Head()
		if err != nil {
			return "", true, err
		}
		if !head.Name().IsBranch() {
			return "HEAD\n", true, nil
		}
		return head.Name().Short() + "\n", true, nil
	}
	if len(args) == 3 && args[0] == "--path-format=absolute" && args[1] == "--git-common-dir" {
		repoRoot, err := repoRootForDir(dir, "")
		if err != nil {
			return "", true, err
		}
		commonDir, err := gitCommonDirForRepoRoot(repoRoot)
		if err != nil {
			return "", true, err
		}
		return commonDir + "\n", true, nil
	}
	if len(args) == 2 && args[0] == "--verify" {
		repo, _, err := openRepo(dir)
		if err != nil {
			return "", true, err
		}
		revision := strings.TrimSpace(args[1])
		revision = strings.TrimSuffix(revision, "^{commit}")
		hash, err := repo.ResolveRevision(plumbing.Revision(revision))
		if err != nil {
			return "", true, err
		}
		return hash.String() + "\n", true, nil
	}
	return "", false, nil
}

func gitBranch(dir string, args []string) (string, bool, error) {
	if len(args) == 1 && args[0] == "--show-current" {
		repo, _, err := openRepo(dir)
		if err != nil {
			return "", true, err
		}
		head, err := repo.Head()
		if err != nil {
			return "", true, err
		}
		if head.Name().IsBranch() {
			return head.Name().Short() + "\n", true, nil
		}
		return "\n", true, nil
	}
	return "", false, nil
}

func gitShowRef(dir string, args []string) (string, bool, error) {
	if len(args) == 2 && args[0] == "--verify" {
		repo, _, err := openRepo(dir)
		if err != nil {
			return "", true, err
		}
		name := strings.TrimSpace(args[1])
		ref, err := repo.Reference(plumbing.ReferenceName(name), true)
		if err != nil {
			return "", true, err
		}
		return fmt.Sprintf("%s %s\n", ref.Hash(), name), true, nil
	}
	return "", false, nil
}

func gitRemote(dir string, args []string) (string, bool, error) {
	repo, _, err := openRepo(dir)
	if err != nil {
		return "", true, err
	}
	if len(args) == 0 {
		remotes, err := repo.Remotes()
		if err != nil {
			return "", true, err
		}
		names := make([]string, 0, len(remotes))
		for _, r := range remotes {
			names = append(names, r.Config().Name)
		}
		sort.Strings(names)
		if len(names) == 0 {
			return "", true, nil
		}
		return strings.Join(names, "\n") + "\n", true, nil
	}
	if len(args) == 2 && args[0] == "get-url" {
		remote, err := repo.Remote(args[1])
		if err != nil {
			return "", true, err
		}
		cfg := remote.Config()
		if len(cfg.URLs) == 0 {
			return "", true, errors.New("remote has no URL")
		}
		return strings.TrimSpace(cfg.URLs[0]) + "\n", true, nil
	}
	return "", false, nil
}

type gitRefWithDate struct {
	ShortName string
	When      int64
}

func gitForEachRef(dir string, args []string) (string, bool, error) {
	sortByCommitterDateDesc := false
	formatShort := false
	prefix := ""
	count := 0

	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--sort=-committerdate":
			sortByCommitterDateDesc = true
		case "--format=%(refname:short)":
			formatShort = true
		case "--count":
			if i+1 >= len(args) {
				return "", true, errors.New("missing --count value")
			}
			n, err := strconv.Atoi(strings.TrimSpace(args[i+1]))
			if err != nil {
				return "", true, err
			}
			count = n
			i++
		default:
			if !strings.HasPrefix(args[i], "--") {
				prefix = strings.TrimSpace(args[i])
			}
		}
	}
	if !sortByCommitterDateDesc || !formatShort || prefix == "" {
		return "", false, nil
	}

	repo, _, err := openRepo(dir)
	if err != nil {
		return "", true, err
	}
	iter, err := repo.References()
	if err != nil {
		return "", true, err
	}
	defer iter.Close()

	items := make([]gitRefWithDate, 0, 32)
	_ = iter.ForEach(func(ref *plumbing.Reference) error {
		name := ref.Name().String()
		if !strings.HasPrefix(name, prefix) {
			return nil
		}
		when := int64(0)
		if ref.Type() == plumbing.HashReference {
			if commit, err := repo.CommitObject(ref.Hash()); err == nil {
				when = commit.Committer.When.Unix()
			} else if tagObj, err := repo.TagObject(ref.Hash()); err == nil {
				if commit, cerr := resolveTagToCommit(repo, tagObj); cerr == nil {
					when = commit.Committer.When.Unix()
				}
			}
		}
		short := strings.TrimPrefix(name, "refs/heads/")
		short = strings.TrimPrefix(short, "refs/remotes/")
		items = append(items, gitRefWithDate{ShortName: short, When: when})
		return nil
	})

	sort.SliceStable(items, func(i, j int) bool {
		if items[i].When == items[j].When {
			return items[i].ShortName < items[j].ShortName
		}
		return items[i].When > items[j].When
	})
	if count > 0 && len(items) > count {
		items = items[:count]
	}
	out := make([]string, 0, len(items))
	for _, item := range items {
		out = append(out, item.ShortName)
	}
	if len(out) == 0 {
		return "", true, nil
	}
	return strings.Join(out, "\n") + "\n", true, nil
}

func resolveTagToCommit(repo *git.Repository, tagObj *object.Tag) (*object.Commit, error) {
	target, err := repo.Object(plumbing.AnyObject, tagObj.Target)
	if err != nil {
		return nil, err
	}
	switch obj := target.(type) {
	case *object.Commit:
		return obj, nil
	case *object.Tag:
		return resolveTagToCommit(repo, obj)
	default:
		return nil, errors.New("tag does not resolve to commit")
	}
}

func gitStatusPorcelain(dir string, args []string) (string, bool, error) {
	if len(args) != 1 || args[0] != "--porcelain" {
		return "", false, nil
	}
	repo, _, err := openRepo(dir)
	if err != nil {
		return "", true, err
	}
	wt, err := repo.Worktree()
	if err != nil {
		return "", true, err
	}
	status, err := wt.Status()
	if err != nil {
		return "", true, err
	}
	if status.IsClean() {
		return "", true, nil
	}
	lines := make([]string, 0, len(status))
	for path, fileStatus := range status {
		lines = append(lines, fmt.Sprintf("%c%c %s", fileStatus.Staging, fileStatus.Worktree, path))
	}
	sort.Strings(lines)
	return strings.Join(lines, "\n") + "\n", true, nil
}

func gitFetch(dir string, args []string) (string, bool, error) {
	remoteName := "origin"
	if len(args) >= 1 && strings.TrimSpace(args[0]) != "" {
		remoteName = strings.TrimSpace(args[0])
	}
	repo, _, err := openRepo(dir)
	if err != nil {
		return "", true, err
	}

	endpoint, remoteURL, endpointErr := remoteEndpoint(repo, remoteName)
	if endpointErr != nil {
		return "", true, endpointErr
	}

	opts := &git.FetchOptions{RemoteName: remoteName}
	auth, usedAgent, authErr := fetchAuthMethod(endpoint, remoteURL)
	if authErr != nil {
		return "", true, authErr
	}
	opts.Auth = auth

	err = repo.Fetch(opts)
	if err != nil && usedAgent && isSSHAuthFailure(err) && endpoint != nil && isSSHEndpoint(endpoint) {
		fallbackAuth, fallbackErr := sshKeyFileAuthForEndpoint(endpoint, remoteURL)
		if fallbackErr == nil {
			opts.Auth = fallbackAuth
			err = repo.Fetch(opts)
		}
	}
	if err != nil && !errors.Is(err, git.NoErrAlreadyUpToDate) {
		return "", true, err
	}
	return "", true, nil
}

func fetchAuthMethod(endpoint *transport.Endpoint, remoteURL string) (transport.AuthMethod, bool, error) {
	if endpoint == nil || !isSSHEndpoint(endpoint) {
		return nil, false, nil
	}
	return sshAuthMethodForEndpoint(endpoint, remoteURL)
}

func remoteEndpoint(repo *git.Repository, remoteName string) (*transport.Endpoint, string, error) {
	remote, err := repo.Remote(remoteName)
	if err != nil {
		return nil, "", err
	}
	cfg := remote.Config()
	if cfg == nil || len(cfg.URLs) == 0 {
		return nil, "", fmt.Errorf("remote %q has no URL", remoteName)
	}
	remoteURL := strings.TrimSpace(cfg.URLs[0])
	endpoint, err := transport.NewEndpoint(remoteURL)
	if err != nil {
		return nil, remoteURL, err
	}
	return endpoint, remoteURL, nil
}

func isSSHEndpoint(endpoint *transport.Endpoint) bool {
	if endpoint == nil {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(endpoint.Protocol)) {
	case "ssh", "git+ssh", "ssh+git":
		return true
	default:
		return false
	}
}

func sshAuthMethodForEndpoint(endpoint *transport.Endpoint, remoteURL string) (transport.AuthMethod, bool, error) {
	user := strings.TrimSpace(endpoint.User)
	if user == "" {
		user = strings.TrimSpace(sshConfigGet(endpoint.Host, "User"))
	}
	if user == "" {
		user = "git"
	}

	if auth, err := gitssh.NewSSHAgentAuth(user); err == nil {
		return auth, true, nil
	}
	auth, err := sshKeyFileAuthForUser(endpoint.Host, user, remoteURL)
	return auth, false, err
}

func sshKeyFileAuthForEndpoint(endpoint *transport.Endpoint, remoteURL string) (transport.AuthMethod, error) {
	user := strings.TrimSpace(endpoint.User)
	if user == "" {
		user = strings.TrimSpace(sshConfigGet(endpoint.Host, "User"))
	}
	if user == "" {
		user = "git"
	}
	return sshKeyFileAuthForUser(endpoint.Host, user, remoteURL)
}

func sshKeyFileAuthForUser(host string, user string, remoteURL string) (transport.AuthMethod, error) {
	keyFiles := sshIdentityFiles(host, user)
	var errs []string
	for _, keyPath := range keyFiles {
		auth, err := gitssh.NewPublicKeysFromFile(user, keyPath, "")
		if err == nil {
			return auth, nil
		}
		errs = append(errs, fmt.Sprintf("%s: %v", keyPath, err))
	}
	if len(errs) == 0 {
		return nil, fmt.Errorf("unable to configure ssh auth for %q; no usable keys found", remoteURL)
	}
	return nil, fmt.Errorf("unable to configure ssh auth for %q: %s", remoteURL, strings.Join(errs, "; "))
}

func isSSHAuthFailure(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(strings.TrimSpace(err.Error()))
	return strings.Contains(msg, "unable to authenticate") ||
		strings.Contains(msg, "attempted methods") ||
		strings.Contains(msg, "permission denied (publickey)")
}

func sshIdentityFiles(host string, remoteUser string) []string {
	fromConfig := sshConfigGetAll(host, "IdentityFile")
	expanded := make([]string, 0, len(fromConfig)+6)
	seen := make(map[string]struct{}, len(fromConfig)+6)

	appendIfValid := func(candidate string) {
		path := expandSSHIdentityPath(candidate, host, remoteUser)
		if path == "" {
			return
		}
		if _, ok := seen[path]; ok {
			return
		}
		info, err := os.Stat(path)
		if err != nil || info.IsDir() {
			return
		}
		seen[path] = struct{}{}
		expanded = append(expanded, path)
	}

	for _, candidate := range fromConfig {
		appendIfValid(candidate)
	}
	for _, name := range []string{
		"~/.ssh/id_ed25519",
		"~/.ssh/id_ecdsa",
		"~/.ssh/id_rsa",
		"~/.ssh/id_dsa",
	} {
		appendIfValid(name)
	}
	return expanded
}

func expandSSHIdentityPath(raw string, host string, remoteUser string) string {
	path := strings.TrimSpace(raw)
	path = strings.Trim(path, `"'`)
	if path == "" {
		return ""
	}
	if strings.EqualFold(path, "none") {
		return ""
	}

	path = strings.ReplaceAll(path, "%h", host)
	path = strings.ReplaceAll(path, "%r", remoteUser)
	if localUser := strings.TrimSpace(os.Getenv("USER")); localUser != "" {
		path = strings.ReplaceAll(path, "%u", localUser)
	}
	path = strings.ReplaceAll(path, "%%", "%")

	if strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err != nil || strings.TrimSpace(home) == "" {
			return ""
		}
		path = filepath.Join(home, path[2:])
	}
	if !filepath.IsAbs(path) {
		home, err := os.UserHomeDir()
		if err != nil || strings.TrimSpace(home) == "" {
			return ""
		}
		path = filepath.Join(home, ".ssh", path)
	}
	return filepath.Clean(path)
}

func gitCheckout(dir string, args []string) (string, bool, error) {
	repo, _, err := openRepo(dir)
	if err != nil {
		return "", true, err
	}
	wt, err := repo.Worktree()
	if err != nil {
		return "", true, err
	}

	if len(args) == 1 {
		branch := strings.TrimSpace(args[0])
		if branch == "" {
			return "", true, errors.New("branch name required")
		}
		err = wt.Checkout(&git.CheckoutOptions{
			Branch: plumbing.NewBranchReferenceName(branch),
		})
		return "", true, err
	}
	if len(args) == 3 && args[0] == "-b" {
		newBranch := strings.TrimSpace(args[1])
		baseRef := strings.TrimSpace(args[2])
		if newBranch == "" {
			return "", true, errors.New("branch name required")
		}
		if baseRef == "" {
			baseRef = "HEAD"
		}
		hash, err := repo.ResolveRevision(plumbing.Revision(baseRef))
		if err != nil {
			return "", true, err
		}
		err = wt.Checkout(&git.CheckoutOptions{
			Hash:   *hash,
			Branch: plumbing.NewBranchReferenceName(newBranch),
			Create: true,
		})
		return "", true, err
	}
	return "", false, nil
}

func gitCommonDirForRepoRoot(repoRoot string) (string, error) {
	repoRoot = strings.TrimSpace(repoRoot)
	if repoRoot == "" {
		return "", errNotInGitRepository
	}
	dotGit := filepath.Join(repoRoot, ".git")
	info, err := os.Stat(dotGit)
	if err == nil && info.IsDir() {
		return filepath.Abs(dotGit)
	}
	if err == nil && !info.IsDir() {
		return parseGitdirPointer(dotGit, repoRoot)
	}
	if errors.Is(err, os.ErrNotExist) {
		return "", errNotInGitRepository
	}
	return "", err
}

func parseGitdirPointer(dotGitFile string, repoRoot string) (string, error) {
	data, err := os.ReadFile(dotGitFile)
	if err != nil {
		return "", err
	}
	line := strings.TrimSpace(string(data))
	const prefix = "gitdir:"
	if !strings.HasPrefix(strings.ToLower(line), prefix) {
		return "", fmt.Errorf("invalid .git file format in %s", repoRoot)
	}
	target := strings.TrimSpace(line[len(prefix):])
	if target == "" {
		return "", fmt.Errorf("empty gitdir in %s", repoRoot)
	}
	if !filepath.IsAbs(target) {
		target = filepath.Join(repoRoot, target)
	}
	target = filepath.Clean(target)
	if strings.Contains(target, string(filepath.Separator)+"worktrees"+string(filepath.Separator)) {
		parts := strings.Split(target, string(filepath.Separator)+"worktrees"+string(filepath.Separator))
		if len(parts) > 0 && strings.TrimSpace(parts[0]) != "" {
			return filepath.Clean(parts[0]), nil
		}
	}
	return target, nil
}
