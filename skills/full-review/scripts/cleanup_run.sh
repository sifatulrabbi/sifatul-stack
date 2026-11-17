#!/usr/bin/env bash
set -euo pipefail

target="${1:?usage: cleanup_run.sh <target: pr<n> or branch>}"
target="${target//\//-}"
repo="$(basename "$(git rev-parse --show-toplevel)")"
wt_target="/tmp/full-review-worktrees/$repo/$target"

if [ -d "$wt_target" ]; then
  for wt in "$wt_target"/round-*/*/; do
    [ -d "$wt" ] && git worktree remove --force "$wt" 2>/dev/null || true
  done
  rm -rf "$wt_target"
  rmdir "${wt_target%/*}" 2>/dev/null || true
fi
git worktree prune
echo "Removed worktrees for $target"
