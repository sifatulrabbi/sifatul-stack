# sifatul-stack

One harness-neutral stack of skills, named sub-agents, and global instructions, installed into Claude Code, Codex, and OpenCode by the `sifatul-stack` CLI, which also carries the tools (`sifatul-stack usage`).

## Layout

- `skills/<name>/`: Agent Skills standard (`SKILL.md` with `name` matching the directory, plus `description`). Linked into `~/.claude/skills` (Claude Code) and `~/.agents/skills` (Codex and OpenCode both read it).
- `agents/<agent>.md` + `agents/models.tsv`: one description and prompt per agent (`principal`, `senior`, `junior`, `associate`); the TSV maps agent × harness to model and effort, and its rows define which agents exist. The CLI renders each harness's format from these; never edit the rendered files.
- `instructions/AGENTS.md`: the user's global instructions, linked to `~/.claude/CLAUDE.md`, `~/.codex/AGENTS.md`, and `~/.config/opencode/AGENTS.md`.
- `cli/`: the Go CLI with its own `AGENTS.md`. `stack/` links and renders, `source/` manages the clone and `state.json` (source, chosen harnesses, rendered agents), the rest is `usage`.

## How installs work

- The binary carries only logic; the stack content is read at runtime. `setup` clones this repo into `${XDG_DATA_HOME:-~/.local/share}/sifatul-stack/stack` and records it in `state.json` next to it; `update` runs `git pull --ff-only` there and re-syncs. So content changes ship by pushing to `main`; only Go changes need a release.
- `setup --from <checkout>` links a working copy instead (dev mode); `update` then skips the pull.
- `state.json` also records the chosen harnesses (`setup --harness`, or the picker in a terminal); no list means all three. Syncing in link mode unlinks unchosen harnesses, status skips them, and `remove` unlinks every harness. `~/.agents/skills` belongs to Codex and OpenCode together, so it stays while either is chosen.
- `SIFATUL_STACK_REPO` overrides the clone URL (tests and e2e runs).

## Rules

- Keep skills and agent prompts harness-neutral: name the agents (`principal`, `senior`, `junior`, `associate`), never models; reference a skill's own files through its directory, not harness-specific agent definitions.
- Agent descriptions are written into YAML and TOML double-quoted strings, so they must not contain `"` or `\`; prompt bodies must not contain `'''`. The CLI refuses to render otherwise.
- The CLI owns symlinks into the stack source and the agent files named by the tiers that `state.json` records under `agents` (written after every `setup` and `update`). Rendered files carry no ownership marker. `setup` and `update` replace whatever sits at a target path (file, dir, or foreign symlink) without a backup; `remove` deletes only what the CLI owns and restores nothing. Owned links whose skill left the source, and owned agent files whose tier left `models.tsv`, are pruned on `setup` and `update`; files with other names in an agents dir are never touched.
- OpenCode agents install with `disabled: true`; the user switches one on by removing that line, and re-rendering keeps their choice. `opencode.json` cannot re-enable a disabled Markdown agent (checked on OpenCode 2.3).

## Verify

- `make -C cli check` (gofmt, vet, race tests). The `stack` tests render the real `agents/` and `instructions/` from this repo against throwaway homes; the `cmd` tests drive setup and update against a real local git upstream.
- `cli/bin/sifatul-stack status` is read-only against the real home.
- After changing agent formats, confirm each harness still loads them: `opencode debug agents --standalone` with `XDG_CONFIG_HOME` pointing at a temp dir, `codex debug prompt-input` with `CODEX_HOME` pointing at a temp dir, and for Claude Code copy the rendered files into a temp project's `.claude/agents/` and ask `claude -p` which subagents it sees.
