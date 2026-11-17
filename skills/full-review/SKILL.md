---
name: full-review
description: Multi-agent code review -- specialist reviewers chosen by risk, every finding verified by reproduction, one consolidated review. Use when the user asks to review or audit code changes, or to check them before merging.
---

# Full review -- lead

You are the lead. You triage the change, decide which specialists run, launch them, get every finding verified, and write the final review. Specialists and verifiers never edit the user's code and only write inside the round directory. You are the only one who talks to the user and the only one who posts anything.

Run this from a `senior` session. Every launch names one of these agents, never a model, so the skill works in any harness. If your harness lacks one, use its closest model:

| Agent       | Use                                                         | Models                  |
| ----------- | ----------------------------------------------------------- | ----------------------- |
| `senior`    | The lead, every specialist, and every verifier              | Opus, Sol, or similar   |
| `associate` | The scouts that gather context, and the related-PRs check   | Haiku, Luna, or similar |

If the harness can't set a model per agent, use the session model.

Paths below: `SKILL_DIR` is this skill's directory; `ROUND_DIR`, `PREV_ROUND_DIR`, `REVIEW_TREE` and `WORKTREE_BASE` come from step 1. Every file in the round is Markdown in the shapes from `references/findings-format.md`.

## 0. Fresh session

A review is only cold if its reviewer didn't write the code. If this session edited, committed, planned, or implemented any of the code under review, stop and tell the user: `Start a new session in this repo and run full-review on <target>.` There is no override. A freshly spawned sub-agent has no such history and passes.

## 1. Set up the round

Identify base and head without changing the user's checkout:

- **GitHub PR:** `gh pr view <n> --json number,title,body,url,baseRefName,headRefOid`, then `git fetch origin <baseRefName> pull/<n>/head`. Target: `pr<n>`.
- **Local branch:** base is the default branch, head is the branch tip. Target: the branch name.
- **Uncommitted work:** head is `git stash create` (a commit of the working tree that leaves it untouched; fall back to `HEAD` if empty). Untracked files aren't included; ask the user to stage them if they should be reviewed. Target: `<branch>-wip`.

Run `SKILL_DIR/scripts/init_run.sh <target> <head-sha>` from the repo root. Each review of a target is a round; the script resumes an unfinished round at the same head, or starts the next one. It prints:

- `ROUND_DIR`: `<target dir>/round-<N>`, where the target dir is `.planning/full-review/<target>` if the repo has a `.planning` dir, else `/tmp/full-review/<repo>/<target>`. All interim files live here.
- `PREV_ROUND_DIR`: the latest finished round of this target, or empty. If set, this is a re-review: follow "Re-review" below alongside every step.
- `REVIEW_TREE`: a detached worktree at head. Every agent reads code from here, never from the user's checkout.
- `WORKTREE_BASE`: where verifier scratch worktrees go.

```
<target dir>/round-<N>/
  round.md  brief.md  hunks.md  related-prs.md
  jobs/<job>.md            # one per launched agent, written before launch
  notes/<job>.md           # each agent's working memory
  findings/<job>/<n>.md    # one per finding, then _done.md
  verified/<id>.md         # one verdict per finding
  final.md  review.md      # review.md marks the round finished
```

## 2. Triage

1. **Related open PRs** (GitHub PRs only): launch an `associate` now with the job in "Related open PRs" below; it runs while you triage.
2. `python3 SKILL_DIR/scripts/classify_hunks.py --base <base> --head <head-sha> -o ROUND_DIR/hunks.md`. It labels hunks as whitespace, comment, lockfile, generated, rename, docs, binary, or semantic. The labels are heuristics: skim the mechanical section (cheap) and treat anything that isn't really mechanical as semantic. Only semantic hunks go to specialists; lockfile hunks go to the dependencies specialist.
3. Read the PR title and description, linked issue, CI results (`gh pr checks <n>`), and the semantic diff.
4. **Intent check:** does the diff do what the PR claims, and only that? Note missing pieces, unrelated changes, and scope creep.
5. **Risk map:** which parts of the change carry which risks (auth, data, public API, concurrency, rollout…).

Size is semantic surface and risk, never file count: 100 files of renames is small, three files rewriting auth is not. Choose a path:

- **Stop and ask** if the intent clearly doesn't match the diff, or the PR bundles independent changes that should be reviewed separately. Explain, propose a split, and continue only if the user says so.
- **Solo** for a small, low-risk semantic surface: write the brief, then review it yourself following `references/specialist-protocol.md` with output `ROUND_DIR/findings/lead/`, then go to step 5. Your findings get verified like everyone else's.
- **Team** for everything else.

## 3. Brief and routing

Write `ROUND_DIR/brief.md`: base and head, `REVIEW_TREE`, intent (claimed vs actual), risk map, repo convention files to honor (REVIEW.md, CLAUDE.md, CONTRIBUTING.md, if present), what CI and linters already reported so nobody repeats it, related open PRs that overlap this one (number, title, shared files), and what's out of scope. On a re-review, add the "Already decided" section described below.

Each specialist owns its whole concern area across every file it touches: one job per concern, whatever the file count. Launch only specialists whose trigger matches the risk map. Every specialist runs on `senior`: reviewing is judgment and discovery, which a `junior` can't verify its way through. Correctness runs on nearly every team review.

| Specialist      | Launch when the semantic change touches                                                                               |
| --------------- | --------------------------------------------------------------------------------------------------------------------- |
| correctness     | Any logic (default for team reviews)                                                                                  |
| security        | Auth, permissions, input parsing, external requests, file paths, secrets, crypto, user data, queries built from input |
| architecture    | New modules, services or classes; cross-layer imports; moved responsibilities; a new pattern                          |
| concurrency     | Async code, threads, locks, queues, shared mutable state, retries, timeouts                                           |
| data-migrations | Schema files, migrations, backfills, ORM models                                                                       |
| tests           | Any behavior change, or test files: missing regression tests and tests that don't earn their keep                     |
| maintainability | Substantial new or rewritten code (skip for small fixes)                                                              |
| performance     | DB queries, loops over collections, request hot paths, caching, batch jobs                                            |
| api-compat      | Public APIs, exported functions, request/response schemas, events, CLI flags, config formats                          |
| dependencies    | Manifests or lockfiles                                                                                                |
| ops-rollout     | Config, feature flags, logging, metrics, deploy files, anything hard to roll back                                     |

If the risk map shows a concern no specialist covers (billing math, a specific flag rollout, an ML pipeline), fill in `references/adhoc-specialist.md` as `jobs/<job>-role.md` and launch it on `senior`.

## 4. Launch specialists

First, launch an `associate` scout to gather facts for each job (callers and importers of changed symbols, related tests, interface definitions) into `ROUND_DIR/jobs/<job>-context.md`. Facts only: paths, line ranges, snippets.

Then write `ROUND_DIR/jobs/<job>.md` for each specialist:

```markdown
# Job: <job>

Role: SKILL_DIR/agents/<specialist>.md (or ROUND_DIR/jobs/<job>-role.md for ad-hoc)
Protocol: SKILL_DIR/references/specialist-protocol.md
Format: SKILL_DIR/references/findings-format.md
Severity: SKILL_DIR/references/severity-rubric.md
Brief: ROUND_DIR/brief.md
Code: REVIEW_TREE
Context: ROUND_DIR/jobs/<job>-context.md
Notes: ROUND_DIR/notes/<job>.md
Output: ROUND_DIR/findings/<job>/
Scope: <files, with hunk ranges if only part of a file is in this concern>
Other specialists in this run: <names>
Focus: <risk-map items specific to this job>
```

Launch all specialists in parallel, each as its agent with this prompt: `You are a code review specialist. Read your job file at ROUND_DIR/jobs/<job>.md and follow it.`

## 5. Collect

A specialist is finished when its `findings/<job>/_done.md` exists. Read each one:

- **Uncovered**: relaunch that job for the remaining scope.
- **Escalations**: launch the same specialist again for just those hunks (new job, e.g. `security-esc`), so it can go deeper on a narrower scope.
- **Handoffs**: if one points at a concern no running specialist covers, decide whether it needs its own job.

## 6. Verify

Every finding is verified. The user should only see findings that survived an attempt to reproduce them.

1. `python3 SKILL_DIR/scripts/merge_findings.py merge ROUND_DIR --review-tree REVIEW_TREE --worktree-base WORKTREE_BASE`. It folds duplicates from the same specialist, clusters overlapping findings, and writes one verifier job per batch as `ROUND_DIR/jobs/verify-<n>.md`. It refuses while a specialist has no `_done.md` or a finding is malformed, naming the file: relaunch that agent to finish or fix it.
2. For each verifier job, create its scratch worktree: `git worktree add --detach WORKTREE_BASE/verify-<n> <head-sha>`.
3. Launch all verifiers in parallel on `senior` with the same one-line prompt pointing at the job file.
4. `python3 SKILL_DIR/scripts/merge_findings.py final ROUND_DIR` writes `final.md`: kept findings (confirmed and plausible) sorted by severity, alternative directions from kept findings, dropped findings with their reasons, and any finding no verifier reached. Relaunch verifiers for unverified findings, or for a verdict the script refuses, then run it again.

## 7. Write the review

Write `ROUND_DIR/review.md` from `final.md` and the files it points at, and show it in chat:

```markdown
# Review: <PR title or branch>

**Intent:** <what the change does and whether it matches its description>
**Verdict:** <approve | approve with suggestions | changes requested> -- <one-line reason>

## Critical (then High, Medium, Low; omit empty sections)

### <title> -- `path:start-end` · confirmed | plausible

<what's wrong and its impact, 1–3 sentences>
**Evidence:** <reproduction, or for plausible: the traced path and what couldn't be executed>
**Fix:** <the smallest fix>
**Regression test:** <the verifier's regression test, when present>

## Related open PRs

- #<n> <title> -- <conflict in meaning | dependency | text conflict>: <one line>

## Alternative directions

Broader changes worth considering. They don't affect the verdict.

- **<title>** (from <finding title>): <the direction, 1–3 sentences>

---

Reviewed for: <concerns covered>. <N> findings dropped in verification (<refuted>, <unproven>, <duplicates>); reasons in ROUND_DIR/final.md.
```

Write in one voice; don't attribute findings to specialists. Merge findings that turn out to be the same root cause. Omit the related PRs and alternatives sections when empty.

Post to the PR only when the user asks (asking up front counts as confirmation). Summary comment: `gh pr comment <n> --body-file ROUND_DIR/review.md`. Inline: one PR review via `gh api repos/{owner}/{repo}/pulls/<n>/reviews` with a `comments` array, so it lands as a single review. Never approve or request changes on the PR unless asked.

## 8. Clean up

`SKILL_DIR/scripts/cleanup_run.sh <target>` removes the review and scratch worktrees of every round. Keep the round directories.

## Related open PRs

Write `ROUND_DIR/jobs/related-prs.md` and launch an `associate` on it:

```markdown
# Job: related-prs

Code: REVIEW_TREE (head <head-sha>, PR #<n>)
Output: ROUND_DIR/related-prs.md

Facts only; no verdicts.

1. List every open PR except #<n>, drafts included, with no limit: `gh pr list --state open --limit 1000 --json number,title,url,author,isDraft,baseRefName,headRefOid,files`. The `files` list stops at 100; for a PR with 100 files, get the full list with `gh pr diff <m> --name-only`.
2. Keep the PRs that change at least one file this PR changes. For each, `git fetch origin pull/<m>/head`, then record: the shared files; per shared file, the line ranges each PR changes and whether they overlap; functions or symbols both PRs change; and `git merge-tree --write-tree --name-only <head-sha> <their head>` (exit 1 means conflict: list the conflicted files).
3. Write the output as one `## #<m> <title>` section per PR with url, author, draft, base, and the facts above. Start the file with how many open PRs you checked and how many share files. If `gh` or GitHub is unavailable, write one line saying why and stop.
```

When it finishes, judge each PR it lists: **independent**, **text conflict** (merge-tree conflicts, meaning compatible), **conflict in meaning** (merges cleanly or not, but together they behave wrong: both change the same behavior, one relies on what the other changes), or **dependency** (one needs the other merged first). Every verdict except independent becomes a finding in `ROUND_DIR/findings/related-prs/<n>.md` at the overlapping location in this PR, with severity from the rubric and a trigger naming both heads; then write `findings/related-prs/_done.md`. They are verified like every other finding. If the check was skipped, say so in the review footer.

## Re-review

When `PREV_ROUND_DIR` is set, this round reviews what changed since that round's head (in its `round.md`):

- **Already decided.** Add an "Already decided" section to the brief listing every finding from `PREV_ROUND_DIR/final.md`, kept and dropped, with title, location, status, and reason. Specialists don't report these again unless the lines changed or they have new evidence.
- **Routing.** Route specialists to the concerns that `git diff <previous head> <head-sha>` touches, with that diff as their scope.
- **Carried findings.** Kept findings whose lines are unchanged carry over without re-verification. Copy each kept finding whose lines changed into `findings/carried/<n>.md`, with `file` and `lines` moved to where that code sits at this head (then write `findings/carried/_done.md`) so it is verified again.
- **Review.** Say which findings were carried over, which were re-verified, and which earlier findings the new pushes fixed.
