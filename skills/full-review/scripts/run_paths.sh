run_paths() {
  local target="$1" kind name gitdir
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
  gitdir="$(git rev-parse --absolute-git-dir)"
  REPO_ROOT="$(git rev-parse --show-toplevel)"
  REPO_KEY="$(basename "$REPO_ROOT")-$(printf '%s' "$gitdir" | git hash-object --stdin | cut -c1-8)"
  TARGET_KEY="$kind/$name"
  if [ -d "$REPO_ROOT/.planning" ]; then
    TARGET_DIR="$REPO_ROOT/.planning/full-review/$TARGET_KEY"
  else
    TARGET_DIR="/tmp/full-review/$REPO_KEY/$TARGET_KEY"
  fi
  WORKTREES_DIR="/tmp/full-review-worktrees/$REPO_KEY/$TARGET_KEY"
  FIXES_REF="refs/full-review/$REPO_KEY/$TARGET_KEY/fixes"
}
