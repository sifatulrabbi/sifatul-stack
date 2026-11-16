#!/usr/bin/env bash
# Create the run directory and a read-only review worktree at the head commit.
# Usage (from the repo root): init_run.sh <run-id> <head-ref>
# Prints RUN_DIR, REVIEW_TREE and WORKTREE_BASE as KEY=VALUE lines.
set -euo pipefail

run_id="${1:?usage: init_run.sh <run-id> <head-ref>}"
head="${2:?usage: init_run.sh <run-id> <head-ref>}"

root="$(git rev-parse --show-toplevel)"
repo="$(basename "$root")"

if [ -d "$root/.planning" ]; then
  run_dir="$root/.planning/full-review/$run_id"
else
  run_dir="/tmp/full-review/$repo/$run_id"
fi
mkdir -p "$run_dir/jobs" "$run_dir/notes" "$run_dir/findings" "$run_dir/verified"

wt_base="/tmp/full-review-worktrees/$repo/$run_id"
review_tree="$wt_base/review"
if [ ! -d "$review_tree" ]; then
  mkdir -p "$wt_base"
  git worktree add --detach "$review_tree" "$head" >/dev/null 2>&1
fi

echo "RUN_DIR=$run_dir"
echo "REVIEW_TREE=$review_tree"
echo "WORKTREE_BASE=$wt_base"
