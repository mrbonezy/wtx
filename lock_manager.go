package main

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
)

type LockManager struct {
	staleAfter time.Duration
}

func NewLockManager() *LockManager {
	return &LockManager{staleAfter: 10 * time.Second}
}

type WorktreeLock struct {
	path         string
	worktreePath string
	repoRoot     string
	ownerID      string
	pid          int
}

var (
	ownerIDOnce   sync.Once
	cachedOwnerID string
)

func (m *LockManager) Acquire(repoRoot string, worktreePath string) (*WorktreeLock, error) {
	return m.acquireWithPID(repoRoot, worktreePath, os.Getpid())
}

func (m *LockManager) AcquireForPID(repoRoot string, worktreePath string, pid int) (*WorktreeLock, error) {
	if pid <= 0 {
		return nil, errors.New("invalid pid")
	}
	return m.acquireWithPID(repoRoot, worktreePath, pid)
}

func (m *LockManager) acquireWithPID(repoRoot string, worktreePath string, pid int) (*WorktreeLock, error) {
	repoRoot = strings.TrimSpace(repoRoot)
	worktreePath = strings.TrimSpace(worktreePath)
	if repoRoot == "" {
		return nil, errors.New("repo root required")
	}
	if worktreePath == "" {
		return nil, errors.New("worktree path required")
	}

	lockPath, err := m.lockPath(repoRoot, worktreePath)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(lockPath), 0o755); err != nil {
		return nil, err
	}

	ownerID := buildOwnerID()
	payload, err := lockPayload(repoRoot, worktreePath, ownerID, pid)
	if err != nil {
		return nil, err
	}

	file, err := os.OpenFile(lockPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err == nil {
		if _, werr := file.Write(payload); werr != nil {
			_ = file.Close()
			_ = os.Remove(lockPath)
			return nil, werr
		}
		_ = file.Close()
		_ = writeWorktreeLastUsed(repoRoot, worktreePath)
		return &WorktreeLock{path: lockPath, worktreePath: worktreePath, repoRoot: repoRoot, ownerID: ownerID, pid: pid}, nil
	}
	if !errors.Is(err, os.ErrExist) {
		return nil, err
	}

	info, statErr := os.Stat(lockPath)
	if statErr != nil {
		return nil, statErr
	}
	current, readErr := readLockPayload(lockPath)
	if readErr == nil && current.PID > 0 && pidAlive(current.PID) {
		if current.OwnerID != ownerID {
			return nil, errors.New("worktree locked")
		}
	}
	if time.Since(info.ModTime()) < m.staleAfter {
		if readErr != nil || current.OwnerID != ownerID {
			return nil, errors.New("worktree locked")
		}
	}

	tmpPath := lockPath + "." + randomToken() + ".tmp"
	if err := os.WriteFile(tmpPath, payload, 0o644); err != nil {
		return nil, err
	}
	if err := os.Rename(tmpPath, lockPath); err != nil {
		_ = os.Remove(tmpPath)
		return nil, err
	}

	current, err = readLockPayload(lockPath)
	if err != nil {
		return nil, err
	}
	if current.OwnerID != ownerID || current.PID != pid {
		return nil, errors.New("worktree locked")
	}
	_ = writeWorktreeLastUsed(repoRoot, worktreePath)
	return &WorktreeLock{path: lockPath, worktreePath: worktreePath, repoRoot: repoRoot, ownerID: ownerID, pid: pid}, nil
}

func (m *LockManager) IsAvailable(repoRoot string, worktreePath string) (bool, error) {
	repoRoot = strings.TrimSpace(repoRoot)
	worktreePath = strings.TrimSpace(worktreePath)
	if repoRoot == "" {
		return false, errors.New("repo root required")
	}
	if worktreePath == "" {
		return false, errors.New("worktree path required")
	}

	lockPath, err := m.lockPath(repoRoot, worktreePath)
	if err != nil {
		return false, err
	}
	info, err := os.Stat(lockPath)
	if err == nil {
		payload, perr := readLockPayload(lockPath)
		if perr != nil {
			return false, nil
		}
		if payload.OwnerID == buildOwnerID() {
			return true, nil
		}
		if payload.PID > 0 && pidAlive(payload.PID) {
			return false, nil
		}
		if time.Since(info.ModTime()) < m.staleAfter {
			return false, nil
		}
		return true, nil
	}
	if errors.Is(err, os.ErrNotExist) {
		return true, nil
	}
	return false, err
}

func (l *WorktreeLock) Release() {
	if l == nil {
		return
	}
	_ = writeWorktreeLastUsed(l.repoRoot, l.worktreePath)
	_ = os.Remove(l.path)
}

func (m *LockManager) ForceUnlock(repoRoot string, worktreePath string) error {
	repoRoot = strings.TrimSpace(repoRoot)
	worktreePath = strings.TrimSpace(worktreePath)
	if repoRoot == "" {
		return errors.New("repo root required")
	}
	if worktreePath == "" {
		return errors.New("worktree path required")
	}
	lockPath, err := m.lockPath(repoRoot, worktreePath)
	if err != nil {
		return err
	}
	if err := os.Remove(lockPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

func (m *LockManager) ReleaseIfOwned(repoRoot string, worktreePath string) error {
	repoRoot = strings.TrimSpace(repoRoot)
	worktreePath = strings.TrimSpace(worktreePath)
	if repoRoot == "" || worktreePath == "" {
		return nil
	}
	lockPath, err := m.lockPath(repoRoot, worktreePath)
	if err != nil {
		return err
	}
	payload, err := readLockPayload(lockPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	if payload.OwnerID != buildOwnerID() {
		return nil
	}
	_ = writeWorktreeLastUsed(repoRoot, worktreePath)
	if err := os.Remove(lockPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

func (l *WorktreeLock) RebindPID(pid int) error {
	if l == nil {
		return errors.New("lock required")
	}
	if pid <= 0 {
		return errors.New("invalid pid")
	}
	current, err := readLockPayload(l.path)
	if err != nil {
		return err
	}
	if current.OwnerID != l.ownerID || current.PID != l.pid {
		return errors.New("lock ownership lost")
	}
	payload, err := lockPayload(l.repoRoot, l.worktreePath, l.ownerID, pid)
	if err != nil {
		return err
	}
	tmpPath := l.path + "." + randomToken() + ".tmp"
	if err := os.WriteFile(tmpPath, payload, 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, l.path); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	_ = writeWorktreeLastUsed(l.repoRoot, l.worktreePath)
	l.pid = pid
	return nil
}

func (m *LockManager) lockPath(repoRoot string, worktreePath string) (string, error) {
	worktreeID, err := worktreeID(repoRoot, worktreePath)
	if err != nil {
		return "", err
	}
	home := strings.TrimSpace(os.Getenv("HOME"))
	if home == "" {
		return "", errors.New("HOME not set")
	}
	lockDir := filepath.Join(home, ".wtx", "locks")
	return filepath.Join(lockDir, worktreeID+".lock"), nil
}

func worktreeID(repoRoot string, worktreePath string) (string, error) {
	repoIDRoot := repoRoot
	if gitPath, err := gitPath(); err == nil {
		commonDir, err := gitOutputInDir(repoRoot, gitPath, "rev-parse", "--path-format=absolute", "--git-common-dir")
		if err == nil && strings.TrimSpace(commonDir) != "" {
			repoIDRoot = commonDir
		}
	}
	repoRootReal, err := realPath(repoIDRoot)
	if err != nil {
		return "", err
	}
	worktreeReal, err := realPathOrAbs(worktreePath)
	if err != nil {
		return "", err
	}

	repoID := hashString(repoRootReal)
	worktreeID := hashString(repoID + ":" + worktreeReal)
	return worktreeID, nil
}

func realPath(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	return filepath.EvalSymlinks(abs)
}

func realPathOrAbs(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	real, err := filepath.EvalSymlinks(abs)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return abs, nil
		}
		return "", err
	}
	return real, nil
}

func writeWorktreeLastUsed(repoRoot string, worktreePath string) error {
	path, err := worktreeLastUsedPath(repoRoot, worktreePath)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	timestamp := time.Now().UTC().Format(time.RFC3339Nano)
	return os.WriteFile(path, []byte(timestamp+"\n"), 0o644)
}

func worktreeLastUsedUnix(repoRoot string, worktreePath string) int64 {
	path, err := worktreeLastUsedPath(repoRoot, worktreePath)
	if err != nil {
		return 0
	}
	info, err := os.Stat(path)
	if err != nil {
		return 0
	}
	return info.ModTime().UnixNano()
}

func worktreeLastUsedPath(repoRoot string, worktreePath string) (string, error) {
	worktreeID, err := worktreeID(repoRoot, worktreePath)
	if err != nil {
		return "", err
	}
	home := strings.TrimSpace(os.Getenv("HOME"))
	if home == "" {
		return "", errors.New("HOME not set")
	}
	lastUsedDir := filepath.Join(home, ".wtx", "last_used")
	return filepath.Join(lastUsedDir, worktreeID), nil
}

func hashString(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func buildOwnerID() string {
	ownerIDOnce.Do(func() {
		cachedOwnerID = computeOwnerID()
	})
	return cachedOwnerID
}

func computeOwnerID() string {
	if explicit := strings.TrimSpace(os.Getenv("WTX_OWNER_ID")); explicit != "" {
		return "explicit:" + explicit
	}
	if strings.TrimSpace(os.Getenv("TMUX")) != "" {
		if sessionID, err := currentSessionID(); err == nil && strings.TrimSpace(sessionID) != "" {
			if windowID, werr := currentWindowID(); werr == nil && strings.TrimSpace(windowID) != "" {
				return "tmux:" + sessionID + ":" + windowID
			}
			return "tmux:" + sessionID
		}
	}
	if session := strings.TrimSpace(os.Getenv("TERM_SESSION_ID")); session != "" {
		return "term-session:" + session
	}
	if pane := strings.TrimSpace(os.Getenv("WEZTERM_PANE")); pane != "" {
		return "wezterm-pane:" + pane
	}
	if window := strings.TrimSpace(os.Getenv("KITTY_WINDOW_ID")); window != "" {
		return "kitty-window:" + window
	}

	name := os.Getenv("USER")
	if name == "" {
		if u, err := user.Current(); err == nil {
			name = u.Username
		}
	}
	host, _ := os.Hostname()
	if name == "" && host == "" {
		name = "unknown"
	}
	if host == "" {
		host = "unknown"
	}
	return fmt.Sprintf("%s@%s:%d:%s", name, host, os.Getpid(), randomToken())
}

type lockPayloadData struct {
	OwnerID string `json:"owner_id"`
	PID     int    `json:"pid"`
}

func lockPayload(repoRoot string, worktreePath string, ownerID string, pid int) ([]byte, error) {
	data := map[string]any{
		"pid":           pid,
		"owner_id":      ownerID,
		"worktree_path": worktreePath,
		"repo_root":     repoRoot,
		"timestamp":     time.Now().UTC().Format(time.RFC3339Nano),
	}
	return json.Marshal(data)
}

func readLockPayload(path string) (lockPayloadData, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return lockPayloadData{}, err
	}
	var payload lockPayloadData
	if err := json.Unmarshal(data, &payload); err != nil {
		return lockPayloadData{}, err
	}
	return payload, nil
}

func pidAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := syscall.Kill(pid, 0)
	if err == nil {
		return true
	}
	if errors.Is(err, syscall.EPERM) {
		return true
	}
	return false
}

func randomToken() string {
	var buf [8]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(buf[:])
}
