#!/usr/bin/env bash
set -euo pipefail

source "$(dirname "$0")/run_paths.sh"
run_paths "${1:?usage: cleanup_run.sh <target: pr/<n>, branch/<name>, or wip/<branch>>}"

if [ -d "$WORKTREES_DIR" ]; then
  for wt in "$WORKTREES_DIR"/round-*/*/; do
    [ -d "$wt" ] && git worktree remove --force "$wt" 2>/dev/null || true
  done
  rm -rf "$WORKTREES_DIR"
  rmdir "${WORKTREES_DIR%/*}" "${WORKTREES_DIR%/*/*}" 2>/dev/null || true
fi
git worktree list --porcelain -z | while IFS= read -r -d '' line; do
  wt="${line#worktree }"
  case "$wt" in
  *"/full-review-worktrees/$REPO_KEY/$TARGET_KEY/round-"*/*)
    git worktree remove --force "$wt" 2>/dev/null || true
    rmdir "${wt%/*}" 2>/dev/null || true
    ;;
  esac
done
git worktree prune
git show-ref --verify --quiet "$FIXES_REF" && git update-ref -d "$FIXES_REF"
echo "Removed worktrees for $1"
