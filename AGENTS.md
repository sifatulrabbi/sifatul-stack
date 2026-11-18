# sifatul-stack

One harness-neutral stack of skills, named sub-agents, and global instructions, installed into Claude Code, Codex, and OpenCode by the `sifatul-stack` CLI, which also carries the tools (`sifatul-stack usage`).

## Layout

- `skills/<name>/`: Agent Skills standard (`SKILL.md` with `name` matching the directory, plus `description`). Linked into `~/.claude/skills` (Claude Code) and `~/.agents/skills` (Codex and OpenCode both read it).
- `agents/<agent>.md` + `agents/models.tsv`: one description and prompt per agent (`principal`, `senior`, `junior`, `associate`); the TSV maps agent × harness to model and effort, and its rows define which agents exist. The CLI renders each harness's format from these; never edit the rendered files.
- `extras.json`: other skill repos the user can add at install, keyed by name, each with `repo`, an optional `branch`, and the `skills` to link by folder name. Unknown keys are rejected.
- `instructions/AGENTS.md`: the user's global instructions, linked to `~/.claude/CLAUDE.md`, `~/.codex/AGENTS.md`, and `~/.config/opencode/AGENTS.md`.
- `cli/`: the Go CLI with its own `AGENTS.md`. `stack/` links and renders, `source/` manages the clone and `state.json` (source, chosen harnesses and extras, rendered agents), the rest is `usage`.

## How installs work

- The binary carries only logic; the stack content is read at runtime. Every `install` clones this repo into `${XDG_DATA_HOME:-~/.local/share}/sifatul-stack/stack` if it is missing, re-clones it when its stored origin URL or checked-out branch no longer matches (refusing when the clone has uncommitted, stashed, or unpushed work, detached commits included), otherwise runs `git pull --ff-only` there, then syncs and records it in `state.json` next to it. So content changes ship by pushing to `main`; only Go changes need a release.
- `install --from <checkout>` links a working copy instead (dev mode) and never pulls it.
- `state.json` also records the chosen harnesses (`install --harness`, or the picker in a terminal); no list means all three. `install` and `status` touch only the chosen harnesses; a harness never chosen is never touched. A harness dropped from the previous choice is cleaned up by that `install`: its owned links and agent files are removed and nothing is restored. `~/.agents/skills` belongs to Codex and OpenCode together, so it stays while either is chosen.
- `install` also picks extras (`--extras`, the picker, or the last choice; none on a first run) and records them in `state.json`. Each chosen extra is cloned into `extras/<name>` next to the stack clone, re-cloned the same way when its URL or branch changes, and pulled on every `install`; its skills link into the same skill dirs as the stack's, and the stack wins a name clash. Dropping an extra prunes its links and deletes its clone. `status` never pulls extras.
- `SIFATUL_STACK_REPO` overrides the clone URL (tests and e2e runs).

## Rules

- Keep skills and agent prompts harness-neutral: name the agents (`principal`, `senior`, `junior`, `associate`), never models; reference a skill's own files through its directory, not harness-specific agent definitions.
- Agent descriptions are written into YAML and TOML double-quoted strings, so they must not contain `"` or `\`; prompt bodies must not contain `'''`. The CLI refuses to render otherwise.
- The CLI owns symlinks into the stack source and the agent files named by the tiers that `state.json` records under `agents` (written after every `install`). Rendered files carry no ownership marker. `install` replaces whatever sits at a target path (file, dir, or foreign symlink) without a backup; cleaning up a dropped harness deletes only what the CLI owns. Owned links whose skill left the source, and owned agent files whose tier left `models.tsv`, are pruned on every `install`; files with other names in an agents dir are never touched.
- OpenCode agents install with `disabled: true`; the user switches one on by removing that line, and re-rendering keeps their choice. `opencode.json` cannot re-enable a disabled Markdown agent (checked on OpenCode 2.3).

## Verify

- `make -C cli check` (gofmt, vet, race tests). The `stack` tests render the real `agents/` and `instructions/` from this repo against throwaway homes; the `cmd` tests drive `install` and `status` against a real local git upstream.
- `cli/bin/sifatul-stack status` is read-only against the real home.
- After changing agent formats, confirm each harness still loads them: `opencode debug agents --standalone` with `XDG_CONFIG_HOME` pointing at a temp dir, `codex debug prompt-input` with `CODEX_HOME` pointing at a temp dir, and for Claude Code copy the rendered files into a temp project's `.claude/agents/` and ask `claude -p` which subagents it sees.

## Releases

Users install the CLI from the Homebrew tap github.com/sifatulrabbi/homebrew-tap (`Formula/sifatul-stack.rb`), which builds `cli/` from a tag's source tarball with Homebrew's `go` and stamps `-X main.version`. Releasing is manual, spans both repos, and happens only when the user asks; only `cli/` changes need one.

1. `sifatul-stack`: on an up-to-date `main` with `make -C cli check` passing, push an annotated `vX.Y.Z` tag, then `gh release create vX.Y.Z --title vX.Y.Z --generate-notes`.
2. Tap, checked out at `$(brew --repository sifatulrabbi/tap)`: `git pull --ff-only`, then `brew bump-formula-pr --write-only --commit --url https://github.com/sifatulrabbi/sifatul-stack/archive/refs/tags/vX.Y.Z.tar.gz sifatulrabbi/tap/sifatul-stack` rewrites `url` and `sha256` and commits. Confirm with `brew audit --strict --online`, `brew upgrade` (or `brew install --build-from-source`), and `brew test` on `sifatulrabbi/tap/sifatul-stack`, then push the tap's `main`.

Keep the `go` line in `cli/go.mod` at or below the Go version Homebrew ships (`brew info go`); a newer one breaks the formula build.
