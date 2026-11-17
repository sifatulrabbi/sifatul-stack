# sifatul-stack CLI

The Go CLI behind sifatul-stack. The stack commands (`install`, `status`) are described in the [root README](../README.md); this file covers `sifatul-stack usage`, which shows the remaining subscription quota for Claude Code and Codex, for both humans and agents.

```sh
make install                # build and install sifatul-stack to ~/.local/bin (or: make install BINDIR=/usr/local/bin)
```

Other targets: `make build` (to `./bin/sifatul-stack`), `make test`, `make check` (gofmt + vet + race tests), `make uninstall`, `make clean`, `make help`.

```sh
./bin/sifatul-stack usage                 # all providers, human table
./bin/sifatul-stack usage --json          # compact one-line JSON, for agents
./bin/sifatul-stack usage --json --pretty # full indented report, for debugging
./bin/sifatul-stack usage codex           # one provider
./bin/sifatul-stack completion zsh        # shell completion (cobra)
```

In a terminal the table is styled with Lip Gloss and spinners show while providers and session logs load. Piped, redirected, or with `NO_COLOR` set, it prints the same layout as plain text with no escape codes.

Exit code: `0` all providers reported, `1` at least one provider failed (its report carries `error`), `2` bad usage.

## How each provider is read

| Provider | Primary source | Fallback | Auth |
|---|---|---|---|
| claude | `GET https://api.anthropic.com/api/oauth/usage` with `anthropic-beta: oauth-2025-04-20` (same data as `/usage` in Claude Code) | none | Claude Code's OAuth token from the macOS Keychain item `Claude Code-credentials`, else `~/.claude/.credentials.json` |
| codex | `codex app-server` JSON-RPC: `initialize` → `initialized` → `account/rateLimits/read` | latest `token_count` event in the most recently modified `~/.codex/sessions/**/rollout-*.jsonl` that has one (marked `source: rollout-cache` with `as_of`) | owned by the Codex binary, which refreshes its own token |

The tool never refreshes or writes credentials. An expired or rejected (HTTP 401/403) Claude token produces an error that says to open Claude Code once. When the Codex app-server outlasts `--timeout`, the rollout fallback still gets its own 2s to read the cache.

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

Window names: `session` (5h) and `weekly` for Codex. Claude uses its own `kind` values (`session`, `weekly_all`, `weekly_scoped:<model>`). Spend windows such as Claude's `extra_usage` (paid overage credits, shown as spent of the monthly limit in the account's currency) are not limits, so they are left out of `--json` and appear only in the text table and in `--json --pretty`. The same goes for `extras` (credits balance, reset credits, live-source errors): they're for people, not agents.

`--json --pretty` prints the unrounded domain report (`used_percent`, `window_minutes`, `as_of`, ...) for debugging.

## Environment

- `USAGE_CODEX_BIN`: path to the codex binary (default `codex` on `PATH`)
- `CODEX_HOME`: Codex home (default `~/.codex`)

## Layout

- `usage/`: domain (`Report`, `Window`, `Provider` port, `Fetch` and concurrent `Collect`)
- `claude/`: credential stores (Keychain, file) and the usage HTTP adapter
- `codex/`: `SnapshotSource` port with app-server and rollout adapters, normalized via `ToReport`
- `render/`: usage text and JSON output, and the stack summary and status views
- `ui/`: shared Lip Gloss styles, column alignment, and the Bubble Tea progress runner (animated only on a terminal)
- `cmd/sifatul-stack`: cobra commands (`root.go`, `stackcmds.go`) and production wiring (`main.go`)

Test fixtures in each package's `testdata/` are real responses captured on 2026-11-16, with identifiers redacted.
