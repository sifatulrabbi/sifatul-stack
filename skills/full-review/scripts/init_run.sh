#!/usr/bin/env bash
set -euo pipefail

usage="usage: init_run.sh <target: pr/<n>, branch/<name>, or wip/<branch>> <head-ref>"
source "$(dirname "$0")/run_paths.sh"
run_paths "${1:?$usage}"
head="$(git rev-parse --verify "${2:?$usage}^{commit}")"

mkdir -p "$TARGET_DIR"

last=0
prev=""
for dir in "$TARGET_DIR"/round-*/ "$WORKTREES_DIR"/round-*/; do
  [ -d "$dir" ] || continue
  n="${dir%/}"
  n="${n##*/round-}"
  [ "$n" -gt "$last" ] && last="$n"
done
for ((n = last; n >= 1; n--)); do
  if [ -f "$TARGET_DIR/round-$n/review.md" ]; then
    prev="$TARGET_DIR/round-$n"
    break
  fi
done

round=$((last + 1))
if [ "$last" -gt 0 ] && [ ! -f "$TARGET_DIR/round-$last/review.md" ] &&
  grep -qx "head: $head" "$TARGET_DIR/round-$last/round.md" 2>/dev/null; then
  round="$last"
fi

round_dir="$TARGET_DIR/round-$round"
mkdir -p "$round_dir/jobs" "$round_dir/notes" "$round_dir/findings" "$round_dir/verified"
[ -f "$round_dir/round.md" ] || printf -- '---\nhead: %s\n---\n' "$head" >"$round_dir/round.md"

wt_base="$WORKTREES_DIR/round-$round"
review_tree="$wt_base/review"
if [ ! -d "$review_tree" ]; then
  mkdir -p "$wt_base"
  git worktree prune
  git worktree add -q --detach "$review_tree" "$head"
fi
if ! git -C "$review_tree" merge-base --is-ancestor "$head" HEAD; then
  echo "review tree $review_tree is not at $head; if $FIXES_REF exists it pins unpushed fixes, so push or discard them before running cleanup_run.sh $1" >&2
  exit 1
fi

echo "ROUND=$round"
echo "ROUND_DIR=$round_dir"
echo "PREV_ROUND_DIR=$prev"
echo "REVIEW_TREE=$review_tree"
echo "WORKTREE_BASE=$wt_base"
echo "FIXES_REF=$FIXES_REF"
