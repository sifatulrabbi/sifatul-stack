# sifatul-stack

My agent stack for Claude Code, Codex, and OpenCode: skills, named sub-agents (`principal`, `senior`, `junior`, `associate`), global instructions, and the tools around them, managed by one CLI.

## Install

Homebrew and AUR packages are on the way. Until then, build from source (Go and git required):

```sh
git clone https://github.com/sifatulrabbi/sifatul-stack && make -C sifatul-stack/cli install   # installs to ~/.local/bin
```

## Use

```sh
sifatul-stack setup     # clone the stack, link skills and global instructions, render the sub-agents
sifatul-stack update    # pull the latest stack and re-sync
sifatul-stack status    # read-only: what is linked, rendered, stale, or in conflict
sifatul-stack remove    # undo setup and restore the backed-up global instructions
sifatul-stack usage     # remaining Claude Code and Codex quota (see cli/README.md)
```

`setup` backs up an existing `~/.claude/CLAUDE.md`, `~/.codex/AGENTS.md`, or `~/.config/opencode/AGENTS.md` before linking the stack's instructions in its place; `remove` puts it back. Anything else already at a target path is reported as a conflict and left alone.

OpenCode sub-agents install switched off (`disabled: true`). Delete that line in `~/.config/opencode/agents/<agent>.md` to switch one on; `update` keeps your choice.

Working on the stack itself? `sifatul-stack setup --from ~/path/to/checkout` links your working copy, so edits show up live.
