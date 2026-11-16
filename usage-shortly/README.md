# usage-shortly

One command to see the remaining subscription quota for Claude Code and Codex, for both humans and agents.

```sh
make install                # build and install usage-shortly to ~/.local/bin (or: make install BINDIR=/usr/local/bin)
usage-shortly prices update # one-time: install LiteLLM model prices for the weekly $ figure
```

Other targets: `make build` (to `./bin/usage-shortly`), `make test`, `make check` (gofmt + vet + race tests), `make uninstall`, `make clean`, `make help`.

```sh
./bin/usage-shortly                 # all providers, human table
./bin/usage-shortly --json          # compact one-line JSON, for agents
./bin/usage-shortly --json --pretty # full indented report, for debugging
./bin/usage-shortly codex           # one provider
./bin/usage-shortly prices update   # install LiteLLM model prices (needed for the weekly $ figure)
./bin/usage-shortly completion zsh  # shell completion (cobra)
```

Exit code: `0` all providers reported, `1` at least one provider failed (its report carries `error`), `2` bad usage.

## How each provider is read

| Provider | Primary source | Fallback | Auth |
|---|---|---|---|
| claude | `GET https://api.anthropic.com/api/oauth/usage` with `anthropic-beta: oauth-2025-04-20` (same data as `/usage` in Claude Code) | none | Claude Code's OAuth token from the macOS Keychain item `Claude Code-credentials`, else `~/.claude/.credentials.json` |
| codex | `codex app-server` JSON-RPC: `initialize` → `initialized` → `account/rateLimits/read` | latest `token_count` event in `~/.codex/sessions/**/rollout-*.jsonl` (marked `source: rollout-cache` with `as_of`) | owned by the Codex binary, which refreshes its own token |

The tool never refreshes or writes credentials. An expired Claude token produces an error that says to open Claude Code once.

Neither usage endpoint is a documented public API. Both are what the official clients call, so field names may drift. The parsers ignore unknown fields, and the Claude parser prefers the newer `limits[]` array, falling back to the legacy `five_hour` / `seven_day*` fields.

## JSON contract (`--json`)

One line, built to cost agents few tokens: whole-number percents, precomputed `left` and `resets_in_s` so agents never do date math, timestamps in UTC to the second, and empty fields left out. Pretty-printed here for reading:

```json
{"fetched_at":"2026-11-16T03:54:24Z","reports":[
  {"provider":"claude","plan":"team (default_claude_max_5x)","windows":[
    {"name":"session","used":2,"left":98,"resets_in_s":15938,"resets_at":"2026-11-16T08:20:00Z"},
    {"name":"weekly_all","used":1,"left":99,"resets_in_s":313318,"resets_at":"2026-11-19T19:00:00Z"}]},
  {"provider":"codex","plan":"plus","windows":[
    {"name":"weekly","used":30,"left":70,"resets_in_s":241591,"resets_at":"2026-11-18T23:00:55Z"}],
   "stale":true,"source":"rollout-cache","age_s":789}]}
```

- `used` / `left`: percent of the window, rounded. `left` is `100 - used`, never below 0.
- `resets_in_s`: seconds until the window resets, measured from `fetched_at` and never below 0. Absent when the provider reports no reset time.
- `stale`, `source`, `age_s`: present only when the live source failed and the numbers come from a cache. `age_s` is how old the cached snapshot is.
- `error`: present only when a provider failed. The process then exits 1.

Window names: `session` (5h) and `weekly` for Codex. Claude uses its own `kind` values (`session`, `weekly_all`, `weekly_scoped:<model>`). Spend windows such as Claude's `extra_usage` (paid overage credits) are not limits, so they are left out of `--json` and appear only in the text table and in `--json --pretty`. The same goes for `extras` (credits balance, reset credits, live-source errors): they're for people, not agents.

`--json --pretty` prints the unrounded domain report (`used_percent`, `window_minutes`, `as_of`, ...) for debugging.

## Weekly API-equivalent cost (human view only)

The table and `--json --pretty` show what this week's tokens would have cost at pay-as-you-go API prices. Compact `--json` leaves it out and skips the log scan entirely.

- **Week:** the provider's weekly limit window, i.e. `resets_at - 7d` until now. Providers without a weekly window get no figure.
- **Tokens:** read from local logs. Claude: `~/.claude/projects/**/*.jsonl`, deduplicated by message id + request id, with cache writes split into 5-minute and 1-hour tiers. Codex: `~/.codex/sessions/**/rollout-*.jsonl`, using deltas of the running `total_token_usage`, attributed to the model from the latest `turn_context`.
- **Prices:** LiteLLM's `model_prices_and_context_window.json`, installed with `usage-shortly prices update` into the user cache directory (`~/Library/Caches/usage-shortly/` on macOS). A download that doesn't parse never replaces the installed file. Models without a price are listed as `unpriced` with their token count, never guessed. Models LiteLLM doesn't list can be mapped to another model's price in `priceAliases` (`cmd/usage-shortly/main.go`). Today `codex-auto-review` is priced as `gpt-6.1-terra`, and the table marks it `(priced as gpt-6.1-terra)`.
- **It's a lower bound:** it covers only this machine's logs, so usage from claude.ai web, desktop or mobile, Codex cloud tasks and other machines isn't counted.

## Environment

- `USAGE_CODEX_BIN`: path to the codex binary (default `codex` on `PATH`)
- `CODEX_HOME`: Codex home (default `~/.codex`)
- `CLAUDE_CONFIG_DIR`: Claude Code config dir (default `~/.claude`), used for the transcript logs

## Layout

- `usage/`: domain (`Report`, `Window`, `Provider` port, concurrent `Collect`)
- `claude/`: credential stores (Keychain, file), the usage HTTP adapter, and the transcript token log
- `codex/`: `SnapshotSource` port with app-server and rollout adapters, normalized via `ToReport`, plus the rollout token log
- `cost/`: token/price domain (`Entry`, `Price`, `PriceBook` and `Log` ports, `Summarize`)
- `litellm/`: LiteLLM price list parsing and the local price store (`prices update`)
- `render/`: text and JSON output
- `cmd/usage-shortly`: cobra commands (`root.go`), weekly cost attachment (`weekly.go`), and production wiring (`main.go`)

Test fixtures in each package's `testdata/` are real responses captured on 2026-11-16, with identifiers redacted.
