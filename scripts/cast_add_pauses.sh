#!/usr/bin/env bash
set -euo pipefail

CAST_PATH="${1:-docs/casts/cast-a-new-task-start.cast}"
if [ ! -f "$CAST_PATH" ]; then
  echo "cast not found: $CAST_PATH" >&2
  exit 1
fi

TMP_PATH="${CAST_PATH}.tmp"

perl -pe '
  BEGIN {
    $p_loading = 0;
    $p_pr_list = 0;
    $p_statusline = 0;
    $p_prompt = 0;
  }

  # Insert pause before first loading frame.
  if (!$p_loading && /Loading\.\.\./) {
    s/^\[([0-9]+(?:\.[0-9]+)?)/"[" . sprintf("%.3f", $1 + 1.700)/e;
    $p_loading = 1;
  }

  # Pause when first branch PR number appears (#128).
  if (!$p_pr_list && /#128/) {
    s/^\[([0-9]+(?:\.[0-9]+)?)/"[" . sprintf("%.3f", $1 + 1.500)/e;
    $p_pr_list = 1;
  }

  # Pause when bottom statusline first shows PR #131 summary.
  if (!$p_statusline && /PR #131 \| CI ok 1\/1 \| Review 1\/1/) {
    s/^\[([0-9]+(?:\.[0-9]+)?)/"[" . sprintf("%.3f", $1 + 1.800)/e;
    $p_statusline = 1;
  }

  # Pause right after typed sentence is complete (on final "k").
  if (!$p_prompt && /k\\u001b\\\[K\\u001b\\\[18;119H/) {
    s/^\[([0-9]+(?:\.[0-9]+)?)/"[" . sprintf("%.3f", $1 + 1.400)/e;
    $p_prompt = 1;
  }
' "$CAST_PATH" > "$TMP_PATH"

mv "$TMP_PATH" "$CAST_PATH"
echo "Added pauses to: $CAST_PATH"
