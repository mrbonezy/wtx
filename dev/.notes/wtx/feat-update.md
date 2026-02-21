## WTX Update MVP (Tag-driven, Go-install based)

### Summary
Implement update detection + update command without self-managed binaries/Homebrew.
Use GitHub tags as source of truth, and install with `go install` pinned to a discovered version.

### Scope
1. Add `wtx update` command.
2. Add startup update check for interactive + non-interactive invocations.
3. Add GitHub Action to auto-bump/tag on PR merge to `main`.

### Command/API Changes
1. `wtx update`
- Resolves latest version tag from `mrbonezy/wtx`.
- Runs:
  - `GOPROXY=direct go install github.com/mrbonezy/wtx@vX.Y.Z`
- If install fails with checksum/sumdb path issues, retry once with:
  - `GONOSUMDB=github.com/mrbonezy/wtx`
- Prints clear success/failure and target version.

2. Optional flags (MVP-safe)
- `--check` (only check, no install)
- `--quiet` (machine-friendly output)

### Invocation-time Detection
1. Run non-blocking check on both:
- default interactive invocation (`wtx`)
- direct command invocation (`wtx checkout ...`, etc.)
2. Do not check for internal/helper commands:
- hidden tmux status/title/agent commands
- completion generation
3. Throttle checks via local state file (`~/.wtx/update-state.json`):
- default interval: 24h
4. Timeout: short (2-3s), never blocks normal command flow.
5. If newer exists, print one-line notice to stderr:
- `wtx <current> -> <latest> available. Run: wtx update`

### Version Source
1. Canonical latest = GitHub tags/releases for `mrbonezy/wtx`.
2. Parse semver tags only (`vX.Y.Z`); ignore malformed/pre-release unless explicitly enabled later.
3. Compare against compiled `version` var.

### CI/Release Automation
1. New GitHub Action on merge/push to `main`:
- Determine bump level from PR labels (default `patch`).
- Compute next semver tag.
- Create/push tag.
2. Optional: update `version.go` in a release PR/commit (or continue injecting via ldflags if you prefer that flow).

### Acceptance Criteria
1. After merge + tag, `wtx update --check` shows new version from GitHub.
2. `wtx update` upgrades to that exact version via pinned `@vX.Y.Z`.
3. Startup check never blocks UX and is rate-limited.
4. No dependence on GitHub release binaries or Homebrew.

### Assumptions / Defaults
1. Distribution remains Go-install based for now.
2. No self-replacing binary logic in MVP.
3. No Homebrew support in MVP.
