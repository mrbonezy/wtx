# WTX

WTX minimizes worktree churn in large monorepos by reusing and locking worktrees while launching AI agents on the right branch instantly.

## Why WTX

- Open your preferred TUI agent on the correct branch/worktree in one command.
- Reuse existing worktrees to avoid expensive re-bootstrap/re-index cycles.
- Lock active worktrees to prevent branch collisions during concurrent agent sessions.
- Surface PR + CI state in-terminal via statusline.
- Make branch lifecycle predictable: create, continue, deliver, clean up.

## Demo

Watch these in order (under 2 minutes total).

### Cast A: Fastest New-Task Start (20-30s)

Interactive new-branch flow from picker -> `codex` launch, with locked + recent branch context visible.

```bash
wtx
# Enter on <new branch>
# Branch name: feat/interactive_cast_a
# Enter through defaults until launch
```

Local cast file: `docs/casts/cast-a-new-task-start.cast`

```bash
asciinema play docs/casts/cast-a-new-task-start.cast
```

### Cast B: Context Switching (15-25s)

`Ctrl+A` actions popup -> open shell context split.

```bash
# inside a wtx-managed tmux session
# press Ctrl+A, choose: Open shell (split down)
```

[Asciinema fallback link](https://asciinema.org/)

```text
[asciinema cast placeholder: docs/casts/cast-b-context-switch.cast]
```

### Cast C: PR + CI Visibility in Statusline (20-35s)

Show `gh` PR state and CI checks in tmux statusline without leaving terminal.

```bash
gh pr create --fill
gh pr checks --watch
```

[Asciinema fallback link](https://asciinema.org/)

```text
[asciinema cast placeholder: docs/casts/cast-c-pr-ci-statusline.cast]
```

### Cast D (Optional): Reuse + Locking Safety (25-40s)

Show worktree reuse plus locked-branch safety under concurrent sessions.

```bash
wco feature/existing
wco feature/existing
# second attempt shows branch/worktree is in use (locked)
```

[Asciinema fallback link](https://asciinema.org/)

```text
[asciinema cast placeholder: docs/casts/cast-d-reuse-locking.cast]
```

## Installation And Requirements

### Supported environment

- macOS or Linux
- `zsh` recommended (completion installer targets zsh)
- `tmux` recommended (statusline/actions experience)

### Required dependencies

- `git` (required)
- Go 1.24+ (for `go install`)
- `gh` (recommended for PR + CI data)
- `tmux` (recommended for managed session/statusline)
- Agent CLI (for example `codex`, `claude`, etc.)

### Install

```bash
go install github.com/mrbonezy/wtx@latest
```

Optional completion + aliases:

```bash
wtx completion install --aliases
exec zsh
```

## Quickstart

1. Configure WTX once.
```bash
wtx config
```
Success: `~/.wtx/config.json` exists and includes your `agent_command`.

2. Start new work on a branch.
```bash
wtx checkout -b my_new_branch
# or, if aliases enabled:
wco -b my_new_branch
```
Success: your agent opens in a managed worktree on `my_new_branch`.

3. Resume an existing branch later.
```bash
wtx co my_new_branch
```
Success: WTX reuses the existing branch/worktree slot instead of creating a new one.

4. Ship from terminal.
```bash
gh pr create --fill
gh pr checks --watch
```
Success: PR exists, checks are visible via `gh`, and tmux statusline reflects PR/CI state.

## Core Workflow

### 1) New work

```bash
wtx checkout -b <branch>
# alias: wco -b <branch>
```

Optional base override for new branch creation:

```bash
wtx checkout -b <branch> --from origin/main --fetch
```

### 2) Continue existing work

```bash
wtx co <branch>
```

WTX resolves the target and reuses a compatible worktree when available.

### 3) Open context shell flow

- In a WTX tmux session, press `Ctrl+A` for the actions popup.
- Choose `Open shell (split down)` for quick context shell access.
- Alternative command path:

```bash
wtx shell
```

### 4) PR creation/checks flow

```bash
gh pr create --fill
gh pr checks --watch
```

You can also jump by PR number directly:

```bash
wtx pr <number>
```

### 5) Cleanup flow (unlock/remove/prune)

- Open picker with `wtx`.
- Use `u` to unlock selected in-use worktree (with confirmation).
- Use `d` to delete selected clean worktree (with confirmation).
- Prune stale worktree metadata:

```bash
git worktree prune
```

## Configuration

### Minimal config first

Run:

```bash
wtx config
```

Set at least:

- `agent_command` (for example `codex`)
- defaults for new-branch base behavior

### Advanced config

- Branch base defaults:
  - `new_branch_base_ref`
  - `new_branch_fetch_first`
- Main picker branch list:
  - `main_screen_branch_limit`
- Statusline/integration toggles at runtime:
  - `WTX_DISABLE_TMUX=1`
  - `WTX_DISABLE_ITERM=1`
- Branch/worktree naming patterns:
  - Branch names are your convention.
  - Managed worktrees are created under `<repo>.wt/wt.*`.

### Example config

```json
{
  "agent_command": "codex",
  "new_branch_base_ref": "origin/main",
  "new_branch_fetch_first": true,
  "ide_command": "code",
  "main_screen_branch_limit": 10
}
```

Path: `~/.wtx/config.json`

## Troubleshooting

### `worktree locked`

- Cause: another agent/pane still holds the worktree lock.
- Fix: open `wtx`, select the branch/worktree, press `u` to force unlock.

### Missing dependencies

```bash
command -v git gh tmux
```

If any are missing, install them and rerun `wtx`.

### `gh` authentication issues

```bash
gh auth login && gh auth status
```

### Stale tmux statusline

```bash
tmux refresh-client -S
```

If still stale, re-enter through `wtx` so status bindings are re-applied.

## Recording Guidelines (For Demo Authoring)

1. Keep casts task-scoped; one intent per recording.
2. Use clean terminal theme/font and large text.
3. No typing pauses; rehearse commands beforehand.
4. End every cast on a clear success state.
5. Under each embedded cast, include exact commands shown.

## Deliverables (For Demo/Docs Pass)

1. Final `README.md` with complete structure above.
2. Embedded Asciinema blocks + fallback links.
3. Copy-paste command snippets for each cast.
4. Final pass for brevity and scannability (especially top 30 lines).

## License

See [LICENSE](./LICENSE).
