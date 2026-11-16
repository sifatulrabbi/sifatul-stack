#!/usr/bin/env bash
# Remove the review and scratch worktrees for a run. The run directory is kept.
# Usage (from the repo root): cleanup_run.sh <run-id>
set -euo pipefail

run_id="${1:?usage: cleanup_run.sh <run-id>}"
repo="$(basename "$(git rev-parse --show-toplevel)")"
wt_base="/tmp/full-review-worktrees/$repo/$run_id"

if [ -d "$wt_base" ]; then
  for wt in "$wt_base"/*/; do
    [ -d "$wt" ] && git worktree remove --force "$wt" 2>/dev/null || true
  done
  rm -rf "$wt_base"
fi
git worktree prune
echo "Removed worktrees for $run_id"
