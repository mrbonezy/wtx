#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
CAST_DIR="$ROOT_DIR/docs/casts"
CAST_PATH="$CAST_DIR/cast-a-new-task-start.cast"
REAL_HOME="$HOME"

mkdir -p "$CAST_DIR"

DEMO_DIR="$(mktemp -d /tmp/wtx-cast-a.XXXXXX)"
LOCK_PIDS=()
CONTROL_PID=""
TMUX_REAL_BIN=""
WTX_CAST_TMUX_SOCKET="wtxcast-$$"
cleanup() {
  if [ -n "$CONTROL_PID" ]; then
    kill "$CONTROL_PID" >/dev/null 2>&1 || true
  fi
  for pid in "${LOCK_PIDS[@]:-}"; do
    kill "$pid" >/dev/null 2>&1 || true
  done
  if [ -n "${TMUX_REAL_BIN:-}" ]; then
    "$TMUX_REAL_BIN" -L "$WTX_CAST_TMUX_SOCKET" kill-server >/dev/null 2>&1 || true
  fi
  rm -rf "$DEMO_DIR"
}
trap cleanup EXIT

HOME_DIR="$DEMO_DIR/home"
REPO_DIR="$DEMO_DIR/repo"
WT_ROOT="$DEMO_DIR/repo.wt"
WT_SLOT1="$WT_ROOT/wt.1"
WT_SLOT2="$WT_ROOT/wt.2"
WT_SLOT3="$WT_ROOT/wt.3"
BIN_DIR="$DEMO_DIR/bin"

mkdir -p "$HOME_DIR" "$REPO_DIR" "$WT_ROOT" "$BIN_DIR"

WTX_BIN="$(command -v wtx || true)"
if [ -z "$WTX_BIN" ]; then
  export GOCACHE="$DEMO_DIR/go-cache"
  export GOMODCACHE="$DEMO_DIR/go-mod-cache"
  (
    cd "$ROOT_DIR"
    go build -o "$BIN_DIR/wtx" .
  )
else
  ln -sf "$WTX_BIN" "$BIN_DIR/wtx"
fi

CODEX_BIN="$(command -v codex || true)"
if [ -z "$CODEX_BIN" ]; then
  echo "codex not found in PATH" >&2
  exit 1
fi
ln -sf "$CODEX_BIN" "$BIN_DIR/codex"
NODE_BIN="$(command -v node || true)"
if [ -n "$NODE_BIN" ]; then
  ln -sf "$NODE_BIN" "$BIN_DIR/node"
fi
cat > "$BIN_DIR/codex-agent" <<'CODEX_AGENT'
#!/usr/bin/env bash
set -euo pipefail
export HOME="$WTX_CAST_REAL_HOME"
exec codex "$@"
CODEX_AGENT
chmod +x "$BIN_DIR/codex-agent"

GIT_BIN="$(command -v git)"
TMUX_BIN="$(command -v tmux)"
ASCIINEMA_BIN="$(command -v asciinema)"
ln -sf "$GIT_BIN" "$BIN_DIR/git"
TMUX_REAL_BIN="$TMUX_BIN"
cat > "$BIN_DIR/tmux" <<'TMUX_WRAPPER'
#!/usr/bin/env bash
set -euo pipefail
exec "$TMUX_REAL_BIN" -L "$WTX_CAST_TMUX_SOCKET" "$@"
TMUX_WRAPPER
chmod +x "$BIN_DIR/tmux"
ln -sf "$ASCIINEMA_BIN" "$BIN_DIR/asciinema"

cat > "$BIN_DIR/gh" <<'GH_EOF'
#!/usr/bin/env bash
set -euo pipefail

if [ "${1:-}" = "pr" ] && [ "${2:-}" = "view" ]; then
  branch="${3:-}"
  shift 3 || true
  # Keep UI spinners visible.
  sleep 0.7
  case "$branch" in
    feat/title_upd)
      cat <<'JSON_EOF'
{"number":128,"url":"https://github.com/mrbonezy/wtx/pull/128","headRefName":"feat/title_upd","baseRefName":"main","title":"Title update","isDraft":false,"state":"OPEN","mergeStateStatus":"CLEAN","updatedAt":"2026-02-21T10:20:00Z","mergedAt":"","reviewDecision":"APPROVED","statusCheckRollup":[{"name":"ci","context":"ci","status":"COMPLETED","conclusion":"SUCCESS"}]}
JSON_EOF
      exit 0
      ;;
    feat/interactive_cast_a)
      cat <<'JSON_EOF'
{"number":131,"url":"https://github.com/mrbonezy/wtx/pull/131","headRefName":"feat/interactive_cast_a","baseRefName":"main","title":"Interactive cast","isDraft":false,"state":"OPEN","mergeStateStatus":"CLEAN","updatedAt":"2026-02-21T10:30:00Z","mergedAt":"","reviewDecision":"APPROVED","statusCheckRollup":[{"name":"ci","context":"ci","status":"COMPLETED","conclusion":"SUCCESS"}]}
JSON_EOF
      exit 0
      ;;
    *)
      echo "no pull requests found for branch" >&2
      exit 1
      ;;
  esac
fi

if [ "${1:-}" = "api" ]; then
  echo "[]"
  exit 0
fi

echo "{}"
exit 0
GH_EOF
chmod +x "$BIN_DIR/gh"

export PATH="$BIN_DIR:/usr/bin:/bin:/usr/sbin:/sbin"
export HOME="$HOME_DIR"
export TMUX_REAL_BIN
export WTX_CAST_TMUX_SOCKET
export WTX_CAST_REAL_HOME="$REAL_HOME"
unset WTX_DISABLE_TMUX
export WTX_DISABLE_ITERM=1
export TERM=xterm-256color
export COLORTERM=truecolor
export CLICOLOR_FORCE=1
export FORCE_COLOR=1
unset NO_COLOR

mkdir -p "$HOME_DIR/.wtx"
if [ -f "$HOME/.codex/auth.json" ]; then
  mkdir -p "$HOME_DIR/.codex"
  cp "$HOME/.codex/auth.json" "$HOME_DIR/.codex/auth.json"
fi
cat > "$HOME_DIR/.wtx/config.json" <<'JSON_EOF'
{
  "agent_command": "sleep 300",
  "new_branch_base_ref": "main",
  "new_branch_fetch_first": false,
  "ide_command": "true",
  "main_screen_branch_limit": 5
}
JSON_EOF

git -C "$REPO_DIR" init >/dev/null
git -C "$REPO_DIR" checkout -B main >/dev/null
git -C "$REPO_DIR" config user.email "cast@example.test"
git -C "$REPO_DIR" config user.name "WTX Cast"
printf "demo\n" > "$REPO_DIR/README.md"
git -C "$REPO_DIR" add README.md
GIT_AUTHOR_DATE="2026-02-20T09:00:00Z" GIT_COMMITTER_DATE="2026-02-20T09:00:00Z" git -C "$REPO_DIR" commit -m "init" >/dev/null

make_branch_commit() {
  local branch="$1"
  local ts="$2"
  git -C "$REPO_DIR" checkout -B "$branch" main >/dev/null
  printf "%s\n" "$branch" >> "$REPO_DIR/README.md"
  git -C "$REPO_DIR" add README.md
  GIT_AUTHOR_DATE="$ts" GIT_COMMITTER_DATE="$ts" git -C "$REPO_DIR" commit -m "update $branch" >/dev/null
}

make_branch_commit "bugfix/auth" "2026-02-21T10:00:00Z"
make_branch_commit "feat/update_readme" "2026-02-21T10:05:00Z"
make_branch_commit "bugfix/refresh_delay" "2026-02-21T10:10:00Z"
make_branch_commit "feat/mcp_support" "2026-02-21T10:15:00Z"
make_branch_commit "feat/title_upd" "2026-02-21T10:20:00Z"
make_branch_commit "slot/free" "2026-02-20T09:05:00Z"
git -C "$REPO_DIR" checkout main >/dev/null

git -C "$REPO_DIR" worktree add "$WT_SLOT1" feat/update_readme >/dev/null
git -C "$REPO_DIR" worktree add "$WT_SLOT2" bugfix/auth >/dev/null
git -C "$REPO_DIR" worktree add "$WT_SLOT3" slot/free >/dev/null

(cd "$REPO_DIR" && WTX_DISABLE_TMUX=1 WTX_OWNER_ID=lock1 wtx co feat/update_readme >/dev/null 2>&1) &
LOCK_PIDS+=("$!")
(cd "$REPO_DIR" && WTX_DISABLE_TMUX=1 WTX_OWNER_ID=lock2 wtx co bugfix/auth >/dev/null 2>&1) &
LOCK_PIDS+=("$!")
sleep 2

cat > "$HOME_DIR/.wtx/config.json" <<'JSON_EOF'
{
  "agent_command": "codex-agent",
  "new_branch_base_ref": "main",
  "new_branch_fetch_first": false,
  "ide_command": "true",
  "main_screen_branch_limit": 5
}
JSON_EOF

CONTROLLER_SCRIPT="$DEMO_DIR/controller.sh"
cat > "$CONTROLLER_SCRIPT" <<'CTRL_EOF'
#!/usr/bin/env bash
set -euo pipefail

session="demo"

# Let open-screen load and show spinners/PR numbers.
sleep 2.2

# Enter on <new branch>
tmux send-keys -t "$session:0.0" Enter
sleep 0.9

# Fill branch name and accept defaults.
branch="feat/interactive_cast_a"
for ((i=0; i<${#branch}; i++)); do
  tmux send-keys -t "$session:0.0" -l "${branch:$i:1}"
  sleep 0.065
done
sleep 0.15
tmux send-keys -t "$session:0.0" Enter
sleep 0.25
tmux send-keys -t "$session:0.0" Enter
sleep 0.25
tmux send-keys -t "$session:0.0" Enter

# Keep codex + tmux statusline visible long enough for one refresh cycle.
sleep 5

# In agent mode, type a message char-by-char but do not send it.
msg="Auth is broken on iOS safari, check"
for ((i=0; i<${#msg}; i++)); do
  tmux send-keys -t "$session" -l "${msg:$i:1}"
  sleep 0.055
done
sleep 4

# Exit codex pane and end recording.
tmux send-keys -t "$session" C-c
sleep 0.6
tmux kill-session -t "$session"
CTRL_EOF
chmod +x "$CONTROLLER_SCRIPT"

RECORD_SCRIPT="$DEMO_DIR/record.sh"
cat > "$RECORD_SCRIPT" <<RECORD_EOF
#!/usr/bin/env bash
set -euo pipefail
cd "$REPO_DIR"
"$CONTROLLER_SCRIPT" &
CONTROL_PID="$!"
export CONTROL_PID
exec tmux new-session -A -s demo "cd '$REPO_DIR' && wtx"
RECORD_EOF
chmod +x "$RECORD_SCRIPT"

rm -f "$CAST_PATH"
TERM=xterm-256color COLUMNS=120 LINES=32 asciinema rec --overwrite --window-size 120x32 -i 1 -c "$RECORD_SCRIPT" "$CAST_PATH" >/dev/null

# Trim noisy codex auth prompts while keeping real launch + statusline.
TRIMMED_CAST="${CAST_PATH}.trimmed"
{
  IFS= read -r header || true
  printf '%s\n' "$header"
  while IFS= read -r line; do
    case "$line" in
      *"codex_cli_rs"*|*"Sign in with Device Code"*|*"On a remote headless machine?"*)
        break
        ;;
      *"Welcome to Codex"*|*"Sign in with ChatGPT"*|*"Provide your own API key"*|*"Press Enter to continue"*)
        break
        ;;
      *)
        printf '%s\n' "$line"
        ;;
    esac
  done
  printf '%s\n' '[0.001, "x", "0"]'
} < "$CAST_PATH" > "$TRIMMED_CAST"
mv "$TRIMMED_CAST" "$CAST_PATH"

echo "Recorded: $CAST_PATH"
