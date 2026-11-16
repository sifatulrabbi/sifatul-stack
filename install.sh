#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)"
MARK="# managed by sifatul-stack"
TIERS="principal senior junior associate"
HARNESSES="claude codex opencode"

CLAUDE_DIR="${CLAUDE_CONFIG_DIR:-$HOME/.claude}"
CODEX_DIR="${CODEX_HOME:-$HOME/.codex}"
OPENCODE_DIR="${XDG_CONFIG_HOME:-$HOME/.config}/opencode"
AGENTS_SKILLS_DIR="$HOME/.agents/skills"

mode=""
skip_tools=0
problems=0

usage() {
  cat <<EOF
Usage: install.sh <link|unlink|status> [--skip-tools]

  link     Link skills and global instructions, render the named sub-agents, install tools.
  unlink   Remove everything link created and restore backed-up instructions.
  status   Show what is linked, rendered, stale, or in conflict.

  --skip-tools   Leave usage-shortly alone.
EOF
}

report() { printf '%-12s %-14s %-9s %s\n' "$1" "$2" "$3" "$4"; }

conflict() {
  report "$1" "$2" "$3" "conflict: $4"
  problems=$((problems + 1))
}

link_path() {
  local kind="$1" name="$2" harness="$3" src="$4" dst="$5"
  if [ -L "$dst" ] && [ "$(readlink "$dst")" = "$src" ]; then
    case "$mode" in
      unlink) rm "$dst" && report "$kind" "$name" "$harness" "removed" ;;
      *) report "$kind" "$name" "$harness" "linked" ;;
    esac
    return
  fi
  if [ "$mode" = unlink ]; then
    return
  fi
  if [ -e "$dst" ] || [ -L "$dst" ]; then
    conflict "$kind" "$name" "$harness" "$dst exists"
    return
  fi
  if [ "$mode" = status ]; then
    report "$kind" "$name" "$harness" "missing"
    return
  fi
  mkdir -p "$(dirname "$dst")"
  ln -s "$src" "$dst"
  report "$kind" "$name" "$harness" "linked"
}

link_instructions() {
  local harness="$1" dst="$2" src="$ROOT/instructions/AGENTS.md" backup
  if [ "$mode" = unlink ]; then
    if [ -L "$dst" ] && [ "$(readlink "$dst")" = "$src" ]; then
      rm "$dst"
      backup="$(ls -1 "$dst".backup-* 2>/dev/null | tail -n 1 || true)"
      if [ -n "$backup" ]; then
        mv "$backup" "$dst"
        report instructions AGENTS.md "$harness" "restored $(basename "$backup")"
      else
        report instructions AGENTS.md "$harness" "removed"
      fi
    fi
    return
  fi
  if [ "$mode" = link ] && [ -f "$dst" ] && [ ! -L "$dst" ]; then
    backup="$dst.backup-$(date +%Y%m%d%H%M%S)"
    mv "$dst" "$backup"
    report instructions AGENTS.md "$harness" "backed up to $(basename "$backup")"
  fi
  if [ "$mode" = status ] && [ -f "$dst" ] && [ ! -L "$dst" ]; then
    report instructions AGENTS.md "$harness" "missing (link backs up $dst)"
    return
  fi
  link_path instructions AGENTS.md "$harness" "$src" "$dst"
}

frontmatter_value() { awk -v key="$2" 'NR > 1 && $0 == "---" { exit } index($0, key ": ") == 1 { print substr($0, length(key) + 3) }' "$1"; }
body() { awk 'seen >= 2 { print; next } $0 == "---" { seen++ }' "$1"; }

prompt() {
  body "$1"
  printf '\n'
  cat "$ROOT/agents/shared.md"
}

tier_model() {
  awk -F '\t' -v tier="$1" -v harness="$2" -v col="$3" '$1 == tier && $2 == harness { print $col }' "$ROOT/agents/models.tsv"
}

render() {
  local tier="$1" harness="$2" enabled="$3" src="$ROOT/agents/$1.md" desc model effort
  desc="$(frontmatter_value "$src" description)"
  model="$(tier_model "$tier" "$harness" 3)"
  effort="$(tier_model "$tier" "$harness" 4)"
  if [ -z "$desc" ] || [ -z "$model" ] || [ -z "$effort" ]; then
    echo "agents/$tier.md or agents/models.tsv lacks a description, model, or effort for $harness" >&2
    exit 2
  fi
  case "$desc" in *'"'* | *'\'*)
    echo "agents/$tier.md: description must not contain double quotes or backslashes" >&2
    exit 2
    ;;
  esac
  if prompt "$src" | grep -qF "'''"; then
    echo "agents/$tier.md or agents/shared.md: prompts must not contain three single quotes" >&2
    exit 2
  fi
  case "$harness" in
    claude)
      printf -- '---\n%s\nname: %s\ndescription: "%s"\nmodel: %s\neffort: %s\n---\n' "$MARK" "$tier" "$desc" "$model" "$effort"
      prompt "$src"
      ;;
    codex)
      printf '%s\nname = "%s"\ndescription = "%s"\nmodel = "%s"\nmodel_reasoning_effort = "%s"\n' "$MARK" "$tier" "$desc" "$model" "$effort"
      printf "developer_instructions = '''\n"
      prompt "$src"
      printf "'''\n"
      ;;
    opencode)
      printf -- '---\n%s\ndescription: "%s"\nmode: subagent\nmodel: %s\nreasoningEffort: %s\n' "$MARK" "$desc" "$model" "$effort"
      [ "$enabled" = on ] || printf 'disabled: true\n'
      printf -- '---\n'
      prompt "$src"
      ;;
  esac
}

agent_path() {
  case "$2" in
    claude) echo "$CLAUDE_DIR/agents/$1.md" ;;
    codex) echo "$CODEX_DIR/agents/$1.toml" ;;
    opencode) echo "$OPENCODE_DIR/agents/$1.md" ;;
  esac
}

opencode_switch() {
  if [ ! -f "$1" ]; then
    echo off
  elif grep -qx 'disabled: true' "$1"; then
    echo off
  else
    echo on
  fi
}

sync_agent() {
  local tier="$1" harness="$2" dst switch="on" state wanted
  dst="$(agent_path "$tier" "$harness")"
  if [ -e "$dst" ] && ! grep -qx "$MARK" "$dst"; then
    [ "$mode" = unlink ] || conflict agent "$tier" "$harness" "$dst is not managed by sifatul-stack"
    return
  fi
  if [ "$mode" = unlink ]; then
    if [ -e "$dst" ]; then
      rm "$dst"
      report agent "$tier" "$harness" "removed"
    fi
    return
  fi
  [ "$harness" = opencode ] && switch="$(opencode_switch "$dst")"
  wanted="$(render "$tier" "$harness" "$switch")"
  if [ ! -e "$dst" ]; then
    state="missing"
  elif [ "$(cat "$dst")" = "$wanted" ]; then
    state="current"
  else
    state="stale"
  fi
  if [ "$mode" = link ] && [ "$state" != current ]; then
    mkdir -p "$(dirname "$dst")"
    printf '%s\n' "$wanted" >"$dst"
    state="written"
  fi
  [ "$harness" = opencode ] && state="$switch, $state"
  report agent "$tier" "$harness" "$state"
}

sync_tools() {
  [ "$skip_tools" = 1 ] && return
  case "$mode" in
    link) make -s -C "$ROOT/usage-shortly" install && report tool usage-shortly - "installed" ;;
    unlink) make -s -C "$ROOT/usage-shortly" uninstall && report tool usage-shortly - "removed" ;;
    status)
      if command -v usage-shortly >/dev/null 2>&1; then
        report tool usage-shortly - "on PATH at $(command -v usage-shortly)"
      else
        report tool usage-shortly - "missing"
      fi
      ;;
  esac
}

main() {
  case "${1:-}" in
    link | unlink | status) mode="$1" ;;
    -h | --help) usage && exit 0 ;;
    *) usage >&2 && exit 2 ;;
  esac
  shift
  for arg in "$@"; do
    case "$arg" in
      --skip-tools) skip_tools=1 ;;
      *) usage >&2 && exit 2 ;;
    esac
  done

  local skill name tier harness
  for skill in "$ROOT"/skills/*/; do
    skill="${skill%/}"
    name="$(basename "$skill")"
    link_path skill "$name" claude "$skill" "$CLAUDE_DIR/skills/$name"
    link_path skill "$name" agents "$skill" "$AGENTS_SKILLS_DIR/$name"
  done

  link_instructions claude "$CLAUDE_DIR/CLAUDE.md"
  link_instructions codex "$CODEX_DIR/AGENTS.md"
  link_instructions opencode "$OPENCODE_DIR/AGENTS.md"

  for tier in $TIERS; do
    for harness in $HARNESSES; do
      sync_agent "$tier" "$harness"
    done
  done

  sync_tools

  if [ "$problems" -gt 0 ]; then
    echo "$problems conflict(s) left untouched; resolve them and re-run." >&2
    exit 1
  fi
}

main "$@"
