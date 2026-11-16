# usage-shortly

Go CLI that reports remaining Claude Code and Codex subscription quota for humans (table) and agents (`--json`). Layout, flags, sources, and the JSON contract live in `README.md`; read it before changing behavior.

## Workflow

- `make check` must pass before you call work done (gofmt, vet, race tests).
- Ports sit at real I/O boundaries only: `usage.Provider`, `codex.SnapshotSource`, `cost.Log`, `cost.PriceBook`, the clock, and process/HTTP starters. A new harness is a new package implementing `usage.Provider`, wired in `cmd/usage-shortly/main.go`.
- Every vendor-format finding gets a regression test, ideally against a real captured payload in that package's `testdata/` with identifiers redacted.
- When delegating to Codex, request `gpt-6.1-sol` at `medium` effort; use `high` only when the task clearly needs it, and say why.

## Audience split (product decision)

Compact `--json` is the agent view and stays minimal: limit windows only, whole-number percents, precomputed `left` and `resets_in_s`. Spend windows (`Window.Spend`, e.g. Claude `extra_usage`), `extras`, and the weekly `$` cost are human-only, so they belong in the text table and `--json --pretty`. Compact `--json` skips the cost log scan entirely.

## Vendor facts

Neither usage source is a documented API; both are what the official clients call.

- **Claude usage**: `GET api.anthropic.com/api/oauth/usage` with `anthropic-beta: oauth-2025-04-20`. Prefer the `limits[]` array; it carries model-scoped limits (`weekly_scoped:<model>`) the legacy `five_hour`/`seven_day*` fields lack. The response is full of null codename fields; ignore unknowns.
- **Claude credentials**: Keychain item `Claude Code-credentials` holds many `mcpOAuth` tokens; read only `claudeAiOauth`. Treat credentials as read-only: refreshing rotates the token Claude Code itself depends on.
- **Codex usage**: `codex app-server --listen stdio://`, then `initialize` → `initialized` → `account/rateLimits/read`. Codex owns auth and refresh. Name windows by `windowDurationMins`, never by slot: a Plus plan reports its weekly window in `primary` with `secondary` null.
- **Codex rollouts** (`~/.codex/sessions/**/rollout-*.jsonl`): `token_count` events repeat with unchanged totals, so cost uses deltas of `total_token_usage`. OpenAI semantics: `input_tokens` includes cached, `output_tokens` includes reasoning.
- **Claude transcripts** (`~/.claude/projects/**/*.jsonl`): roughly half the usage lines are duplicates; dedupe by `message.id` + `requestId`. Skip model `<synthetic>`.
- **Prices**: LiteLLM's price list, installed by `usage-shortly prices update`. Unknown models stay `unpriced`; map them explicitly in `priceAliases` (`cmd/usage-shortly/main.go`).

## Environment gotchas

- Codex's own sandbox blocks the app-server (`Operation not permitted`) and network (DNS). Run live Codex checks from this session, not inside a Codex task.
- The auto-mode classifier may block raw `curl` calls that carry the Claude OAuth token. Verify live through the built binary (`./bin/usage-shortly claude --json`) or ask the user.

## Known gaps

- Weekly cost covers this machine's logs only (a lower bound).
- Unverified: resumed Codex sessions might replay earlier `token_count` events into a new rollout file and double count.
