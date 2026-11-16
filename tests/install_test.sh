#!/usr/bin/env bash
set -uo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd -P)"
INSTALL="$ROOT/install.sh"
TIERS="principal senior junior associate"
passed=0
failed=0

check() {
  local name="$1"
  shift
  if "$@" >/dev/null 2>&1; then
    passed=$((passed + 1))
  else
    failed=$((failed + 1))
    echo "FAIL: $name"
  fi
}

new_home() {
  local h
  h="$(mktemp -d)"
  echo "$h"
}

run() {
  local home="$1"
  shift
  env -u CLAUDE_CONFIG_DIR -u CODEX_HOME -u XDG_CONFIG_HOME HOME="$home" bash "$INSTALL" "$@"
}

points_to() { [ -L "$1" ] && [ "$(readlink "$1")" = "$2" ]; }
yaml_frontmatter_ok() { ruby -ryaml -e 'YAML.safe_load(File.read(ARGV[0]).split(/^---$/)[1]) || exit(1)' "$1"; }
toml_ok() { python3 -I -c 'import sys,tomllib; tomllib.load(open(sys.argv[1],"rb"))' "$1"; }
has_line() { grep -qx -- "$2" "$1"; }
lacks_line() { ! grep -qx -- "$2" "$1"; }

test_link_fresh_home() {
  local h
  h="$(new_home)"
  mkdir -p "$h/.claude"
  echo "old global" >"$h/.claude/CLAUDE.md"

  check "link succeeds on a fresh home" run "$h" link --skip-tools

  for s in "$ROOT"/skills/*/; do
    s="${s%/}"
    check "claude skill $(basename "$s") linked" points_to "$h/.claude/skills/$(basename "$s")" "$s"
    check "agents skill $(basename "$s") linked" points_to "$h/.agents/skills/$(basename "$s")" "$s"
  done

  check "claude instructions linked" points_to "$h/.claude/CLAUDE.md" "$ROOT/instructions/AGENTS.md"
  check "codex instructions linked" points_to "$h/.codex/AGENTS.md" "$ROOT/instructions/AGENTS.md"
  check "opencode instructions linked" points_to "$h/.config/opencode/AGENTS.md" "$ROOT/instructions/AGENTS.md"
  check "existing CLAUDE.md backed up" grep -qx "old global" "$h"/.claude/CLAUDE.md.backup-*

  for t in $TIERS; do
    check "claude $t frontmatter parses" yaml_frontmatter_ok "$h/.claude/agents/$t.md"
    check "claude $t named" has_line "$h/.claude/agents/$t.md" "name: $t"
    check "codex $t toml parses" toml_ok "$h/.codex/agents/$t.toml"
    check "codex $t named" has_line "$h/.codex/agents/$t.toml" "name = \"$t\""
    check "opencode $t frontmatter parses" yaml_frontmatter_ok "$h/.config/opencode/agents/$t.md"
    check "opencode $t is a subagent" has_line "$h/.config/opencode/agents/$t.md" "mode: subagent"
    check "opencode $t starts off" has_line "$h/.config/opencode/agents/$t.md" "disabled: true"
    for f in "$h/.claude/agents/$t.md" "$h/.codex/agents/$t.toml" "$h/.config/opencode/agents/$t.md"; do
      check "$f carries the shared lines" grep -qF "Use any available .planning/ dir as your scratchpad" "$f"
    done
  done
  check "codex senior prompt names its level" grep -q "^You are a senior engineer" "$h/.codex/agents/senior.toml"
  check "claude senior uses opus" has_line "$h/.claude/agents/senior.md" "model: opus"
  check "codex associate uses luna" has_line "$h/.codex/agents/associate.toml" 'model = "gpt-6-luna"'
  check "codex principal runs medium" has_line "$h/.codex/agents/principal.toml" 'model_reasoning_effort = "medium"'
  check "opencode junior uses terra" has_line "$h/.config/opencode/agents/junior.md" "model: openai/gpt-6.1-terra"

  check "second link is a no-op success" run "$h" link --skip-tools
  rm -rf "$h"
}

test_opencode_choice_survives_relink() {
  local h f
  h="$(new_home)"
  run "$h" link --skip-tools >/dev/null 2>&1
  f="$h/.config/opencode/agents/associate.md"
  grep -vx "disabled: true" "$f" >"$f.tmp" && mv "$f.tmp" "$f"

  check "relink after enabling associate succeeds" run "$h" link --skip-tools
  check "associate stays on after relink" lacks_line "$f" "disabled: true"
  check "senior stays off after relink" has_line "$h/.config/opencode/agents/senior.md" "disabled: true"
  check "status reports associate on" sh -c "env -u CLAUDE_CONFIG_DIR -u CODEX_HOME -u XDG_CONFIG_HOME HOME='$h' bash '$INSTALL' status | grep -E '^agent +associate +opencode +on'"
  check "status reports senior off" sh -c "env -u CLAUDE_CONFIG_DIR -u CODEX_HOME -u XDG_CONFIG_HOME HOME='$h' bash '$INSTALL' status | grep -E '^agent +senior +opencode +off'"
  rm -rf "$h"
}

test_stale_managed_file_is_rerendered() {
  local h f
  h="$(new_home)"
  run "$h" link --skip-tools >/dev/null 2>&1
  f="$h/.claude/agents/senior.md"
  sed 's/^model: opus$/model: haiku/' "$f" >"$f.tmp" && mv "$f.tmp" "$f"

  check "status flags the stale agent" sh -c "env -u CLAUDE_CONFIG_DIR -u CODEX_HOME -u XDG_CONFIG_HOME HOME='$h' bash '$INSTALL' status | grep -E '^agent +senior +claude +stale'"
  check "relink restores the model" sh -c "env -u CLAUDE_CONFIG_DIR -u CODEX_HOME -u XDG_CONFIG_HOME HOME='$h' bash '$INSTALL' link --skip-tools && grep -qx 'model: opus' '$f'"
  rm -rf "$h"
}

test_conflicts_are_left_alone() {
  local h
  h="$(new_home)"
  mkdir -p "$h/.agents/skills/full-review" "$h/.claude/agents"
  echo "mine" >"$h/.agents/skills/full-review/SKILL.md"
  echo "my own associate agent" >"$h/.claude/agents/associate.md"

  check "link exits non-zero on conflicts" sh -c "! env -u CLAUDE_CONFIG_DIR -u CODEX_HOME -u XDG_CONFIG_HOME HOME='$h' bash '$INSTALL' link --skip-tools"
  check "foreign skill dir untouched" grep -qx "mine" "$h/.agents/skills/full-review/SKILL.md"
  check "foreign agent file untouched" grep -qx "my own associate agent" "$h/.claude/agents/associate.md"
  check "non-conflicting agents still installed" has_line "$h/.claude/agents/senior.md" "name: senior"
  rm -rf "$h"
}

test_unlink_restores_home() {
  local h
  h="$(new_home)"
  mkdir -p "$h/.claude/agents"
  echo "old global" >"$h/.claude/CLAUDE.md"
  echo "keep me" >"$h/.claude/agents/mine.md"
  run "$h" link --skip-tools >/dev/null 2>&1

  check "unlink succeeds" run "$h" unlink --skip-tools
  check "claude skill link removed" test ! -e "$h/.claude/skills/full-review"
  check "agents skill link removed" test ! -e "$h/.agents/skills/full-review"
  check "managed claude agent removed" test ! -e "$h/.claude/agents/senior.md"
  check "managed codex agent removed" test ! -e "$h/.codex/agents/senior.toml"
  check "managed opencode agent removed" test ! -e "$h/.config/opencode/agents/senior.md"
  check "unmanaged agent kept" grep -qx "keep me" "$h/.claude/agents/mine.md"
  check "CLAUDE.md restored from backup" sh -c "[ ! -L '$h/.claude/CLAUDE.md' ] && grep -qx 'old global' '$h/.claude/CLAUDE.md'"
  check "codex instructions link removed" test ! -e "$h/.codex/AGENTS.md"
  rm -rf "$h"
}

test_rejects_unknown_command() {
  check "unknown command exits non-zero" sh -c "! bash '$INSTALL' bogus"
}

test_link_fresh_home
test_opencode_choice_survives_relink
test_stale_managed_file_is_rerendered
test_conflicts_are_left_alone
test_unlink_restores_home
test_rejects_unknown_command

echo "$passed passed, $failed failed"
[ "$failed" -eq 0 ]
