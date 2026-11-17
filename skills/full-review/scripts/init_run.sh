#!/usr/bin/env bash
set -euo pipefail

usage="usage: init_run.sh <target: pr<n> or branch> <head-ref>"
target="${1:?$usage}"
target="${target//\//-}"
head="$(git rev-parse --verify "${2:?$usage}^{commit}")"

root="$(git rev-parse --show-toplevel)"
repo="$(basename "$root")"

if [ -d "$root/.planning" ]; then
  target_dir="$root/.planning/full-review/$target"
else
  target_dir="/tmp/full-review/$repo/$target"
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

wt_base="/tmp/full-review-worktrees/$repo/$target/round-$round"
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
echo "FIXES_REF=refs/full-review/$target/fixes"
