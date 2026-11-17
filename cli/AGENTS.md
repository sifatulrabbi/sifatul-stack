# sifatul-stack CLI

Go CLI that installs the stack (`stack/`, `source/`; see the root `AGENTS.md` for how installs work) and reports remaining Claude Code and Codex subscription quota for humans (table) and agents (`--json`). The usage layout, flags, sources, and JSON contract live in `README.md`; read it before changing `usage` behavior.

## Workflow

- `make check` must pass before you call work done (gofmt, vet, race tests).
- Packages stay flat at the module root; no `internal/`.
- Ports sit at real I/O boundaries only: `source.Git`, `usage.Provider`, `codex.SnapshotSource`, the clock, and process/HTTP starters. A new harness is a new package implementing `usage.Provider`, wired in `cmd/sifatul-stack/main.go`.
- Every vendor-format finding gets a regression test, ideally against a real captured payload in that package's `testdata/` with identifiers redacted.
- When delegating to Codex, request `gpt-6.1-sol` at `medium` effort; use `high` only when the task clearly needs it, and say why.

## Audience split (product decision)

Styling is for people: `ui.Progress` animates only when stdout is a terminal (decided once in `main.go`), and `ui.Print` downsamples colors to what the writer supports, so pipes, agents, and tests always get plain text. Compact `--json` is the agent view and stays minimal: limit windows only, whole-number percents, precomputed `left` and `resets_in_s`. Spend windows (`Window.Spend`, e.g. Claude `extra_usage`) and `extras` are human-only, so they belong in the text table and `--json --pretty`. There is deliberately no cost figure: API-equivalent cost from local logs was too inaccurate to show and was removed.

## Vendor facts

Neither usage source is a documented API; both are what the official clients call.

- **Claude usage**: `GET api.anthropic.com/api/oauth/usage` with `anthropic-beta: oauth-2025-04-20`. Prefer the `limits[]` array; it carries model-scoped limits (`weekly_scoped:<model>`) the legacy `five_hour`/`seven_day*` fields lack. The response is full of null codename fields; ignore unknowns.
- **Claude credentials**: Keychain item `Claude Code-credentials` holds many `mcpOAuth` tokens; read only `claudeAiOauth`. Treat credentials as read-only: refreshing rotates the token Claude Code itself depends on.
- **Codex usage**: `codex app-server --listen stdio://`, then `initialize` → `initialized` → `account/rateLimits/read`. Codex owns auth and refresh. Name windows by `windowDurationMins`, never by slot: a Plus plan reports its weekly window in `primary` with `secondary` null.
- **Codex rollouts** (`~/.codex/sessions/**/rollout-*.jsonl`): the fallback when the app-server is unavailable; the latest `token_count` event carries the rate limits.

## Environment gotchas

- Codex's own sandbox blocks the app-server (`Operation not permitted`) and network (DNS). Run live Codex checks from this session, not inside a Codex task.
- The auto-mode classifier may block raw `curl` calls that carry the Claude OAuth token. Verify live through the built binary (`./bin/sifatul-stack usage claude --json`) or ask the user.
