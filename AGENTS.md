# sifatul-stack

One harness-neutral stack of skills, named sub-agents, global instructions, and tools, installed into Claude Code, Codex, and OpenCode by `install.sh`.

## Layout

- `skills/<name>/`: Agent Skills standard (`SKILL.md` with `name` matching the directory, plus `description`). Linked into `~/.claude/skills` (Claude Code) and `~/.agents/skills` (Codex and OpenCode both read it).
- `agents/<agent>.md` + `agents/models.tsv`: one description and prompt per agent (`principal`, `senior`, `junior`, `associate`); the TSV maps agent × harness to model and effort. `install.sh` renders each harness's format from these; never edit the rendered files.
- `instructions/AGENTS.md`: the user's global instructions, linked to `~/.claude/CLAUDE.md`, `~/.codex/AGENTS.md`, and `~/.config/opencode/AGENTS.md`.
- `usage-shortly/`: Go CLI with its own `AGENTS.md`; `install.sh` runs its `make install`.

## Rules

- Keep skills and agent prompts harness-neutral: name the agents (`principal`, `senior`, `junior`, `associate`), never models; reference a skill's own files through its directory, not harness-specific agent definitions.
- Agent descriptions are written into YAML and TOML double-quoted strings, so they must not contain `"` or `\`; prompt bodies must not contain `'''`.
- The installer only touches what it owns: symlinks into this repo and files carrying `# managed by sifatul-stack`. Anything else at a target path is reported as a conflict and left alone.
- OpenCode agents install with `disabled: true`; the user switches one on by removing that line, and re-rendering keeps their choice. `opencode.json` cannot re-enable a disabled Markdown agent (checked on OpenCode 2.3).

## Verify

- `bash tests/install_test.sh` runs the installer against throwaway homes.
- `bash install.sh status` is read-only against the real home.
- After changing agent formats, confirm each harness still loads them: `opencode debug agents --standalone` with `XDG_CONFIG_HOME` pointing at a temp dir, `codex debug prompt-input` with `CODEX_HOME` pointing at a temp dir, and for Claude Code copy the rendered files into a temp project's `.claude/agents/` and ask `claude -p` which subagents it sees.
