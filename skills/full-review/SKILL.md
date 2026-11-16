---
name: full-review
description: Multi-agent code review — specialist reviewers chosen by risk, every finding verified by reproduction, one consolidated review. Use when the user asks to review or audit code changes, or to check them before merging.
---

# Full review — lead

You are the lead. You triage the change, decide which specialists run, launch them, get every finding verified, and write the final review. Specialists and verifiers never edit the user's code and only write inside the run directory. You are the only one who talks to the user and the only one who posts anything.

Run this from a `deep` session. Every agent launch names a model tier, never a model, so the skill works in any harness. Map each tier to whatever your harness offers:

| Tier       | Use                                                 | Models                    |
| ---------- | --------------------------------------------------- | ------------------------- |
| `deep`     | The lead, high-risk specialists, high-stakes checks | Opus, Sol, or similar     |
| `standard` | Everything else                                     | Sonnet, Terra, or similar |
| `fast`     | The scout that gathers context for each job         | Haiku, Luna, or similar   |

If the harness can't set a model per agent, use the session model.

Paths below: `SKILL_DIR` is this skill's directory; `RUN_DIR`, `REVIEW_TREE` and `WORKTREE_BASE` come from step 1.

## 1. Set up the run

Identify base and head without changing the user's checkout:

- **GitHub PR:** `gh pr view <n> --json number,title,body,url,baseRefName,headRefOid`, then `git fetch origin <baseRefName> pull/<n>/head`.
- **Local branch:** base is the default branch, head is the branch tip.
- **Uncommitted work:** head is `git stash create` (a commit of the working tree that leaves it untouched; fall back to `HEAD` if empty). Untracked files aren't included; ask the user to stage them if they should be reviewed.

Run `SKILL_DIR/scripts/init_run.sh <run-id> <head-sha>` from the repo root, with run-id `pr<n>-<short-sha>` or `<branch>-<short-sha>`. It prints:

- `RUN_DIR`: `.planning/full-review/<run-id>` if the repo has a `.planning` dir, else under `/tmp/full-review/`. All interim files live here.
- `REVIEW_TREE`: a detached worktree at head. Every agent reads code from here, never from the user's checkout.
- `WORKTREE_BASE`: where verifier scratch worktrees go.

```
RUN_DIR/
  brief.md  hunks.json
  jobs/<job>.md        # one per launched agent, written by you before launch
  notes/<job>.md       # each agent's working memory
  findings/<job>.json  # specialist output
  merged.json  verified/<batch>.json  final.json  review.md
```

## 2. Triage

1. `python3 SKILL_DIR/scripts/classify_hunks.py --base <base> --head <head-sha> -o RUN_DIR/hunks.json`. It labels hunks as whitespace, comment, lockfile, generated, rename, docs, binary, or semantic. The labels are heuristics: skim the non-semantic hunks (cheap) and treat anything that isn't really mechanical as semantic. Only semantic hunks go to specialists; lockfile hunks go to the dependencies specialist.
2. Read the PR title and description, linked issue, CI results (`gh pr checks <n>`), and the semantic diff.
3. **Intent check:** does the diff do what the PR claims, and only that? Note missing pieces, unrelated changes, and scope creep.
4. **Risk map:** which parts of the change carry which risks (auth, data, public API, concurrency, rollout…).

Size is semantic surface and risk, never file count: 100 files of renames is small, three files rewriting auth is not. Choose a path:

- **Stop and ask** if the intent clearly doesn't match the diff, or the PR bundles independent changes that should be reviewed separately. Explain, propose a split, and continue only if the user says so.
- **Solo** for a small, low-risk semantic surface: review it yourself following `references/specialist-protocol.md` with output `findings/lead.json`, then go to step 5. Your findings get verified like everyone else's.
- **Team** for everything else.

## 3. Brief and routing

Write `RUN_DIR/brief.md`: base and head, `REVIEW_TREE`, intent (claimed vs actual), risk map, repo convention files to honor (REVIEW.md, CLAUDE.md, CONTRIBUTING.md, if present), what CI and linters already reported so nobody repeats it, and what's out of scope.

Each specialist owns its whole concern area across every file it touches: one job per concern, whatever the file count. Launch only specialists whose trigger matches the risk map. Correctness runs on nearly every team review.

| Specialist      | Tier       | Launch when the semantic change touches                                                                               |
| --------------- | ---------- | --------------------------------------------------------------------------------------------------------------------- |
| correctness     | `deep`     | Any logic (default for team reviews)                                                                                  |
| security        | `deep`     | Auth, permissions, input parsing, external requests, file paths, secrets, crypto, user data, queries built from input |
| architecture    | `deep`     | New modules, services or classes; cross-layer imports; moved responsibilities; a new pattern                          |
| concurrency     | `deep`     | Async code, threads, locks, queues, shared mutable state, retries, timeouts                                           |
| data-migrations | `deep`     | Schema files, migrations, backfills, ORM models                                                                       |
| tests           | `standard` | Any behavior change, or test files                                                                                    |
| maintainability | `standard` | Substantial new or rewritten code (skip for small fixes)                                                              |
| performance     | `standard` | DB queries, loops over collections, request hot paths, caching, batch jobs                                            |
| api-compat      | `standard` | Public APIs, exported functions, request/response schemas, events, CLI flags, config formats                          |
| dependencies    | `standard` | Manifests or lockfiles                                                                                                |
| ops-rollout     | `standard` | Config, feature flags, logging, metrics, deploy files, anything hard to roll back                                     |

If the risk map shows a concern no specialist covers (billing math, a specific flag rollout, an ML pipeline), fill in `references/adhoc-specialist.md` as `jobs/<job>-role.md` and launch it on `standard`, or `deep` if high-risk.

## 4. Launch specialists

First, launch a `fast` scout to gather facts for each job (callers and importers of changed symbols, related tests, interface definitions) into `RUN_DIR/jobs/<job>-context.md`. Facts only: paths, line ranges, snippets.

Then write `RUN_DIR/jobs/<job>.md` for each specialist:

```markdown
# Job: <job>

Role: SKILL_DIR/agents/<specialist>.md (or RUN_DIR/jobs/<job>-role.md for ad-hoc)
Protocol: SKILL_DIR/references/specialist-protocol.md
Schema: SKILL_DIR/references/findings-schema.md
Severity: SKILL_DIR/references/severity-rubric.md
Brief: RUN_DIR/brief.md
Code: REVIEW_TREE
Context: RUN_DIR/jobs/<job>-context.md
Notes: RUN_DIR/notes/<job>.md
Output: RUN_DIR/findings/<job>.json
Scope: <files, with hunk ranges if only part of a file is in this concern>
Other specialists in this run: <names>
Focus: <risk-map items specific to this job>
```

Launch all specialists in parallel, each on its tier with this prompt: `You are a code review specialist. Read your job file at RUN_DIR/jobs/<job>.md and follow it.`

## 5. Collect

Read each findings file when its specialist finishes:

- `uncovered` not empty: relaunch that job for the remaining scope.
- `escalations`: launch the same specialist on `deep` for just those hunks (new job, e.g. `security-esc`).
- `handoffs`: if one points at a concern no running specialist covers, decide whether it needs its own job.

## 6. Verify

Every finding is verified. The user should only see findings that survived an attempt to reproduce them.

1. `python3 SKILL_DIR/scripts/merge_findings.py RUN_DIR merge` writes `merged.json`: duplicates folded, overlapping findings clustered, and verification batches planned (`deep` for any cluster with a critical or high finding, `standard` otherwise).
2. For each batch, create a scratch worktree: `git worktree add --detach WORKTREE_BASE/<batch> <head-sha>`.
3. Write `RUN_DIR/jobs/verify-<batch>.md` with: Role `SKILL_DIR/agents/verifier.md`, Schema, Severity, Brief, Code, Findings (`RUN_DIR/merged.json`, ids from the batch), Scratch (the worktree path), Notes, Output `RUN_DIR/verified/<batch>.json`.
4. Launch all verifiers in parallel on each batch's tier with the same one-line prompt pointing at the job file.
5. `python3 SKILL_DIR/scripts/merge_findings.py RUN_DIR final` writes `final.json`: kept findings (confirmed and plausible) sorted by severity, plus dropped counts.

## 7. Write the review

Write `RUN_DIR/review.md` from `final.json` and show it in chat:

```markdown
# Review: <PR title or branch>

**Intent:** <what the change does and whether it matches its description>
**Verdict:** <approve | approve with suggestions | changes requested> — <one-line reason>

## Critical (then High, Medium, Low; omit empty sections)

### <title> — `path:start-end` · confirmed | plausible

<what's wrong and its impact, 1–3 sentences>
**Evidence:** <reproduction, or for plausible: the traced path and what couldn't be executed>
**Fix:** <suggestion>

---

Reviewed for: <concerns covered>. <N> findings dropped in verification (<refuted>, <unproven>, <duplicates>); details in RUN_DIR.
```

Write in one voice; don't attribute findings to specialists. Merge findings that turn out to be the same root cause.

Post to the PR only when the user asks (asking up front counts as confirmation). Summary comment: `gh pr comment <n> --body-file RUN_DIR/review.md`. Inline: one PR review via `gh api repos/{owner}/{repo}/pulls/<n>/reviews` with a `comments` array, so it lands as a single review. Never approve or request changes on the PR unless asked.

## 8. Clean up

`SKILL_DIR/scripts/cleanup_run.sh <run-id>` removes the review and scratch worktrees. Keep `RUN_DIR`.

## Re-review after new pushes

If a run dir for an earlier head of the same PR exists, review only what changed since that head: route specialists to the concerns the new changes touch, carry over kept findings on unchanged code, and re-verify carried findings whose lines changed. Say in the review which findings were carried over.
