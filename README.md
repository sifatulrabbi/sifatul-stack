# sifatul-stack

My agent stack for Claude Code, Codex, and OpenCode: skills, named sub-agents (`principal`, `senior`, `junior`, `associate`), global instructions, and the tools around them, run by one CLI.

## Install

Homebrew and AUR packages are on the way. Until then, build from source (Go and git required):

```sh
git clone https://github.com/sifatulrabbi/sifatul-stack && make -C sifatul-stack/cli install   # installs to ~/.local/bin
```

## Use

```sh
sifatul-stack setup     # pick harnesses, clone the stack, link skills and global instructions, render the sub-agents
sifatul-stack update    # pull the latest stack and re-sync
sifatul-stack status    # read-only: what is linked, rendered, stale, or failed
sifatul-stack remove    # undo setup: remove the links and sub-agent files it created
sifatul-stack usage     # remaining Claude Code and Codex quota (see cli/README.md)
```

In a terminal, `setup` asks which harnesses to install into (Claude Code, Codex, OpenCode), starting from your last choice, or on a first run from the ones it finds on this machine. `setup --harness claude,codex` skips the question for scripts and agents; without a terminal or the flag, `setup` keeps the last choice (all three on a first run). `update` and `status` stick to the chosen harnesses, and a harness you drop on a later `setup` is cleaned up the way `remove` would.

`setup` and `update` replace whatever already sits at a target path, with no backup: an existing `~/.claude/CLAUDE.md`, `~/.codex/AGENTS.md`, or `~/.config/opencode/AGENTS.md`, a skill of the same name in `~/.claude/skills` or `~/.agents/skills`, and agent files named `principal`, `senior`, `junior`, or `associate`. Save anything you want to keep first. `remove` deletes only what the stack created and restores nothing; your own agent files with other names are never touched.

OpenCode sub-agents install switched off (`disabled: true`). Delete that line in `~/.config/opencode/agents/<agent>.md` to switch one on; `update` keeps your choice.

Working on the stack itself? `sifatul-stack setup --from ~/path/to/checkout` links your working copy, so edits show up live.
