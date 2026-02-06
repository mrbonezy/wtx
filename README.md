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

Inside the picker:

- `left`/`right`: switch between worktree view and PR view
- `enter`: actions for selected free worktree
- `s`: open shell in selected free worktree
- `d`: delete selected worktree (with confirmation)
- `u`: unlock selected locked worktree (with confirmation)
- `p`: open selected worktree PR URL (worktree view)
- `P`: open selected PR URL (PR view)
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
- Force-unlock flow for locked worktrees (`u`)
- Orphaned worktree detection and disabled selection
- Branch list filtering + recently-used ordering
- Live local status polling in the root menu (`1s`)
- GitHub integration (if `gh` is installed):
  - PR link
  - CI status and progress
  - review summary
  - open PR view ordered by most recently updated
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
