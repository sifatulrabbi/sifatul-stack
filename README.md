# sifatul-stack

My agent stack for Claude Code, Codex, and OpenCode: skills, named sub-agents (`principal`, `senior`, `junior`, `associate`), global instructions, and the tools around them, run by one CLI.

## Install

Homebrew and AUR packages are on the way. Until then, build from source (Go and git required):

```sh
git clone https://github.com/sifatulrabbi/sifatul-stack && make -C sifatul-stack/cli install   # installs to ~/.local/bin
```

## Use

```sh
sifatul-stack install   # pull the latest stack, then link skills and global instructions and render the sub-agents into the chosen harnesses
sifatul-stack status    # read-only: what is linked, rendered, stale, or failed
sifatul-stack usage     # remaining Claude Code and Codex quota (see cli/README.md)
```

Every `install` pulls the latest stack first (cloning it on the first run, or re-cloning it when its origin or branch changed; it refuses instead when the clone holds uncommitted, stashed, or unpushed work). In a terminal, it asks which harnesses to install into (Claude Code, Codex, OpenCode), starting from your last choice, or on a first run from the ones it finds on this machine. `install --harness claude,codex` skips the question for scripts and agents; without a terminal or the flag, `install` keeps the last choice (all three on a first run). A harness you leave out that was chosen before is cleaned up by that `install`: its links and sub-agent files are removed and nothing is restored. A harness you never chose is never touched, and `status` covers only the chosen ones.

In the chosen harnesses, `install` replaces whatever already sits at a target path, with no backup: an existing `~/.claude/CLAUDE.md`, `~/.codex/AGENTS.md`, or `~/.config/opencode/AGENTS.md`, a skill of the same name in `~/.claude/skills` or `~/.agents/skills`, and agent files named `principal`, `senior`, `junior`, or `associate`. Save anything you want to keep first. Your own agent files with other names are never touched.

OpenCode sub-agents install switched off (`disabled: true`). Delete that line in `~/.config/opencode/agents/<agent>.md` to switch one on; later installs keep your choice.

Extras are other people's skill repos, listed in [`extras.json`](extras.json) by name with a `repo`, an optional `branch` (the repo's default branch when left out), and the `skills` to link. In a terminal, `install` asks which extras to add after the harnesses, starting from your last choice (none on a first run); `install --extras mattpocock` or `--extras none` skips the question, and without a terminal or the flag it keeps the last choice. Each chosen extra is cloned into `~/.local/share/sifatul-stack/extras/<name>`, or pulled on every `install`, and each listed skill is found by its folder name anywhere in the repo and linked next to the stack's own skills, replacing whatever sits there. The stack's own skills win a name clash. An extra you drop has its links and its clone removed; a repo or branch change in `extras.json` re-clones it, unless the clone holds uncommitted, stashed, or unpushed work.

Working on the stack itself? `sifatul-stack install --from ~/path/to/checkout` links your working copy, so edits show up live; `install` never pulls it.
