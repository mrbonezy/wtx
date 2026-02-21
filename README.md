# wtx

`wtx` is an interactive CLI for managing and jumping between Git worktrees.

## Install

```sh
go install github.com/mrbonezy/wtx@latest
```

Yes, `go install github.com/mrbonezy/wtx@latest` is still the correct install command.

## Prerequisites

- `git` (required)
- `tmux` (optional; used for pane splitting and status line integration)
- `gh` (optional, for PR/CI/review data)
- iTerm2 (optional, for tab title/color integration)

## Initialize

```sh
wtx init
```

## Usage

```sh
wtx
```

Direct checkout flow (same worktree selection behavior as interactive mode):

```sh
wtx checkout <existing_branch>
wtx co <existing_branch>
wtx pr <pr_number>
```

Create a new branch:

```sh
wtx checkout -b <new_branch>
wtx checkout -b <new_branch> --from origin/main --fetch
```

Check for updates:

```sh
wtx update --check
```

Install latest version:

```sh
wtx update
```

Configure defaults:

```sh
wtx config
```

Install zsh completion (aliases optional):

```sh
wtx completion install
wtx completion install --aliases
wtx completion aliases install
wtx completion aliases remove
```

Inside the picker:
- `enter`: actions for selected free worktree
- `s`: open shell in selected free worktree
- `d`: delete selected worktree (with confirmation)
- `u`: unlock selected in-use worktree (with confirmation)
- `p`: open selected worktree PR URL (worktree view)
- `r`: manual refresh (bypasses GH cache)
- `q`: quit

## Features

- Fast interactive worktree selector
- Create worktree from:
  - a new branch name
  - an existing local branch
- Reuse an existing worktree by selecting `Use <branch>`
- Open a shell directly in a selected worktree
- Locking to prevent concurrent worktree use
- Force-unlock flow for in-use worktrees (`u`)
- Orphaned worktree detection and disabled selection
- Branch list filtering + recently-used ordering
- Configurable main-screen branch list size (`wtx config`, default `10`)
- Non-interactive checkout (`wtx checkout` / `wtx co`)
- PR teleport (`wtx pr <number>`)
- Zsh completion status + installer (`wtx completion status`, `wtx completion install`)
- Optional managed zsh aliases (`wco`, `wpr`)
- Live local status polling in the root menu (`1s`)
- GitHub integration (if `gh` is installed):
  - PR link
  - CI status and progress
  - review summary
- Tmux integration:
  - auto-starts inside a fresh tmux session when launched outside tmux
  - custom bottom status line
  - periodic tmux GH/status refresh
- iTerm integration:
  - tab title (`wtx`, then `wtx - <branch>`)
  - tab color set/reset

## Build

```sh
go build -o wtx
```

## End-to-End Tests

```sh
make e2e
```

This builds `./bin/wtx` once and runs E2E scenarios in an isolated `HOME` temp directory.

```sh
make local-e2e
```

`local-e2e` runs additional local-only scenarios (build tag `local_e2e`) that exercise real git fetch/checkout behavior on an isolated temporary repository (no GitHub mocking and no use of this repository as test data).

Useful test env flags:
- `WTX_DISABLE_TMUX=1` disables tmux integration paths.
- `WTX_DISABLE_ITERM=1` disables iTerm escape integration.
- `WTX_TEST_MODE=1` bypasses interactive UI entrypoints with deterministic behavior.
