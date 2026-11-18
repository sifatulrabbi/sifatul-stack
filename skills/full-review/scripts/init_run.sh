#!/usr/bin/env bash
set -euo pipefail

usage="usage: init_run.sh <target: pr/<n>, branch/<name>, or wip/<branch>> <head-ref>"
source "$(dirname "$0")/run_paths.sh"
run_paths "${1:?$usage}"
head="$(git rev-parse --verify "${2:?$usage}^{commit}")"

if [ -d "$REPO_ROOT/.planning" ]; then
  target_dir="$REPO_ROOT/.planning/full-review/$TARGET_KEY"
else
  target_dir="/tmp/full-review/$REPO_KEY/$TARGET_KEY"
fi
mkdir -p "$target_dir"

last=0
prev=""
for dir in "$target_dir"/round-*/; do
  [ -d "$dir" ] || continue
  n="${dir%/}"
  n="${n##*/round-}"
  [ "$n" -gt "$last" ] && last="$n"
done
for ((n = last; n >= 1; n--)); do
  if [ -f "$target_dir/round-$n/review.md" ]; then
    prev="$target_dir/round-$n"
    break
  fi
done

round=$((last + 1))
if [ "$last" -gt 0 ] && [ ! -f "$target_dir/round-$last/review.md" ] &&
  grep -qx "head: $head" "$target_dir/round-$last/round.md" 2>/dev/null; then
  round="$last"
fi

round_dir="$target_dir/round-$round"
mkdir -p "$round_dir/jobs" "$round_dir/notes" "$round_dir/findings" "$round_dir/verified"
[ -f "$round_dir/round.md" ] || printf -- '---\nhead: %s\n---\n' "$head" >"$round_dir/round.md"

wt_base="$WORKTREES_DIR/round-$round"
review_tree="$wt_base/review"
if [ ! -d "$review_tree" ]; then
  mkdir -p "$wt_base"
  git worktree add --detach "$review_tree" "$head" >/dev/null 2>&1
fi

echo "ROUND=$round"
echo "ROUND_DIR=$round_dir"
echo "PREV_ROUND_DIR=$prev"
echo "REVIEW_TREE=$review_tree"
echo "WORKTREE_BASE=$wt_base"
echo "FIXES_REF=$FIXES_REF"
