run_paths() {
  local target="$1" kind name common
  kind="${target%%/*}"
  name="${target#*/}"
  case "$kind" in
  pr | branch | wip) ;;
  *) kind="" ;;
  esac
  if [ -z "$kind" ] || [ "$name" = "$target" ] || [ -z "$name" ]; then
    echo "target must be pr/<n>, branch/<name>, or wip/<branch>, got: $target" >&2
    return 1
  fi
  name="${name//\%/%25}"
  name="${name//\//%2F}"
  common="$(git rev-parse --path-format=absolute --git-common-dir)"
  REPO_ROOT="$(git rev-parse --show-toplevel)"
  REPO_KEY="$(basename "$REPO_ROOT")-$(printf '%s' "$common" | git hash-object --stdin | cut -c1-8)"
  TARGET_KEY="$kind/$name"
  WORKTREES_DIR="/tmp/full-review-worktrees/$REPO_KEY/$TARGET_KEY"
  FIXES_REF="refs/full-review/$TARGET_KEY/fixes"
}
