---
name: full-review
description: Multi-agent code review -- specialist reviewers chosen by risk, every finding proved by test, reproduction, or an auditable judgment, one consolidated review. Use when the user asks to review or audit code changes, or to check them before merging.
---

# Full review -- lead

You are the lead. You triage the change, decide which specialists run, launch them, get every finding verified, and write the final review. With `--auto-fix`, fixers then fix the kept findings and a second round reviews the fixes (step 8). Specialists and verifiers never edit the user's code and only write inside the round directory. You are the only one who talks to the user and the only one who posts anything.

Run this from a `senior` session. Every launch names one of these agents, never a model, so the skill works in any harness. If your harness lacks one, use its closest model:

- `senior` (Opus, Sol, or similar): the lead, every specialist, every verifier, and the senior fixer.
- `junior` (Sonnet, Terra, or similar): the junior fixer under `--auto-fix`.
- `associate` (Haiku, Luna, or similar): the scouts that gather context, and the related-PRs check.

If the harness can't set a model per agent, use the session model.

Paths below: `SKILL_DIR` is this skill's directory; `ROUND_DIR`, `PREV_ROUND_DIR`, `REVIEW_TREE`, `WORKTREE_BASE` and `FIXES_REF` come from step 1. Every file in the round is Markdown in the shapes from `references/findings-format.md`.

## 0. Fresh session

A review is only cold if its reviewer didn't write the code. If this session edited, committed, planned, or implemented any of the code under review, stop and tell the user: `Start a new session in this repo and run full-review on <target>.` There is no override. The specialists and verifiers you launch start with no history of the code, so they pass this check. Commits made by the fixers in step 8 don't count as this session's work: you route and ask, and never write fix code.

## 1. Set up the round

Identify base and head without changing the user's checkout:

- **GitHub PR:** `gh pr view <n> --json number,title,body,url,baseRefName,headRefName,headRefOid`, then `git fetch origin <baseRefName> pull/<n>/head`. Base is `origin/<baseRefName>`, the PR's own base branch: for a PR stacked on another branch that is the branch beneath it, not the default branch. Target: `pr/<n>`.
- **Local branch:** base is the default branch, head is the branch tip. Target: `branch/<name>`.
- **Uncommitted work:** head is `git stash create` (a commit of the working tree that leaves it untouched; fall back to `HEAD` if empty). Untracked files aren't included; ask the user to stage them if they should be reviewed. Target: `wip/<branch>`.

Run `SKILL_DIR/scripts/init_run.sh <target> <head-sha>` from the repo root. Rounds, worktrees, and `FIXES_REF` are keyed by repo and target, so reviews of different repos or different targets never share them. Each review of a target is a round; the script resumes an unfinished round at the same head, or starts the next one. It prints:

- `ROUND_DIR`: `<target dir>/round-<N>`, where the target dir is `.planning/full-review/<target>` if the repo has a `.planning` dir, else `/tmp/full-review/<repo>-<hash>/<target>`. All interim files live here.
- `PREV_ROUND_DIR`: the latest finished round of this target, or empty. If set, this is a re-review: follow "Re-review" below alongside every step.
- `REVIEW_TREE`: a detached worktree at head. Every agent reads code from here, never from the user's checkout.
- `WORKTREE_BASE`: where verifier scratch worktrees go.
- `FIXES_REF`: the ref that pins fixer commits under `--auto-fix`.

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

1. `python3 SKILL_DIR/scripts/classify_hunks.py --base <base> --head <head-sha> -o ROUND_DIR/hunks.md`. It always diffs base to head, on a re-review too. It labels hunks as whitespace, comment, lockfile, generated, rename, docs, binary, or semantic. Instruction files agents read (skills, agent prompts, AGENTS.md) are always semantic. The labels are heuristics: skim the mechanical section (cheap) and treat anything that isn't really mechanical as semantic. Only semantic hunks go to specialists; lockfile hunks go to the dependencies specialist.
2. Read the PR title and description, linked issue, CI results (`gh pr checks <n>`), and the semantic diff.
3. **Related open PRs** (GitHub PRs only): launch an `associate` now with the job in "Related open PRs" below, listing the symbols this PR renames, removes, or changes the signature of; it runs while you finish triage.
4. **Intent check:** does the diff do what the PR claims, and only that? Write each missing piece (a claimed behavior the diff doesn't deliver, a caller or migration it needs and lacks) as a finding in `ROUND_DIR/findings/lead/`, then write its `_done.md`; on the solo path, write `_done.md` after your review instead. Note unrelated changes and scope creep for the brief.
5. **Risk map:** which parts of the change carry which risks (auth, data, public API, concurrency, rollout…).
6. **Decision inventory:** the decisions this change makes that will be costly to reverse: a new module boundary, data shape or schema, wire or file format, protocol, dependency, or concurrency model. One line each with its location, or `none`.

Size is semantic surface and risk, never file count: 100 files of renames is small, three files rewriting auth is not. Choose a path:

- **Stop and ask** if the intent clearly doesn't match the diff, or the PR bundles independent changes that should be reviewed separately. Explain, propose a split, and continue only if the user says so.
- **Solo** for a small, low-risk semantic surface with an empty decision inventory: write the brief, then review it yourself following `references/specialist-protocol.md` with output `ROUND_DIR/findings/lead/`, working through the "How to work" sections of the matching roles in this order: architecture, correctness, instructions (when the change touches text agents or people follow), tests. Then go to step 5. Your findings get verified like everyone else's.
- **Team** for everything else.

## 3. Brief and routing

Write `ROUND_DIR/brief.md`: base and head, `REVIEW_TREE`, intent (claimed vs actual), risk map, decision inventory, repo convention files to honor (REVIEW.md, CLAUDE.md, CONTRIBUTING.md, if present), what CI and linters already reported so nobody repeats it, related open PRs that overlap this one (number, title, shared files, from `related-prs.md`: wait for it), and what's out of scope. On a re-review, add the "Already decided" section described below.

Each specialist owns its whole concern area across every file it touches: one job per concern, whatever the file count. Launch only specialists whose trigger matches the risk map. Every specialist runs on `senior`: reviewing is judgment and discovery, which a `junior` can't verify its way through. Correctness runs on nearly every team review.

Launch each specialist when the semantic change touches:

- **correctness**: Any logic (default for team reviews)
- **security**: Auth, permissions, input parsing, external requests, file paths, secrets, crypto, user data, queries built from input
- **architecture**: New modules, services or classes; cross-layer imports; moved responsibilities; a new pattern; any item in the decision inventory
- **concurrency**: Async code, threads, locks, queues, shared mutable state, retries, timeouts
- **data-migrations**: Schema files, migrations, backfills, ORM models
- **tests**: Any behavior change, or test files: missing regression tests and tests that don't earn their keep
- **maintainability**: Substantial new or rewritten code (skip for small fixes)
- **performance**: DB queries, loops over collections, request hot paths, caching, batch jobs
- **api-compat**: Public APIs, exported functions, request/response schemas, events, CLI flags, config formats
- **dependencies**: Manifests or lockfiles
- **ops-rollout**: Config, feature flags, logging, metrics, deploy files, anything hard to roll back
- **instructions**: Prompts, skills, agent roles, AGENTS.md or CLAUDE.md, runbooks: text an agent or person follows

If the risk map shows a concern no specialist covers (billing math, a specific flag rollout, an ML pipeline), fill in `references/adhoc-specialist.md` as `jobs/<job>-role.md` and launch it on `senior`.

## 4. Launch specialists

First, for each specialist job, write `ROUND_DIR/jobs/<job>-scout.md` and launch an `associate` on it with the prompt `You are a code review scout. Read your job file at ROUND_DIR/jobs/<job>-scout.md and follow it.`

```markdown
# Job: <job>-scout

Code: REVIEW_TREE
Scope: <the specialist's scope>
Output: ROUND_DIR/jobs/<job>-context.md

Facts only, no judgments: paths, line ranges, short snippets. For the code in scope, collect:

- callers and importers of every changed symbol;
- tests that exercise the changed code;
- interface and type definitions the changed code implements or uses.
```

When the scouts finish, write `ROUND_DIR/jobs/<job>.md` for each specialist:

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

First, judge the related open PRs in `related-prs.md` as described in "Related open PRs" below and write their findings.

A specialist is finished when its `findings/<job>/_done.md` exists. Read each one:

- **Uncovered**: launch a new job `<job>-rest` with the same role for the remaining scope; the first job's findings stay where they are.
- **Escalations**: launch the same specialist again for just those hunks (new job, e.g. `security-esc`), so it can go deeper on a narrower scope.
- **Handoffs**: if one points at a concern no running specialist covers, decide whether it needs its own job.

## 6. Verify

Every finding is verified: proved by a test, a run, or a checked fact, or kept as plausible with a judgment record the user can audit when nothing can run. The user sees only findings that survived.

1. `python3 SKILL_DIR/scripts/merge_findings.py merge ROUND_DIR --review-tree REVIEW_TREE --worktree-base WORKTREE_BASE`. It clusters overlapping findings and writes one verifier job per batch as `ROUND_DIR/jobs/verify-<n>.md`, skipping findings that already have a verdict and numbering after existing jobs. It refuses while a specialist has no `_done.md` or a finding is malformed, naming the file: relaunch that agent to finish or fix it, or fix it yourself when you wrote it (`lead/`, `carried/`, `root-cause/`, `related-prs/`).
2. For each new verifier job, create its scratch worktree: `git worktree add --detach WORKTREE_BASE/verify-<n> <head-sha>`.
3. Launch all new verifiers in parallel on `senior`, each with the prompt `You are a code review verifier. Read your job file at ROUND_DIR/jobs/verify-<n>.md and follow it.`
4. `python3 SKILL_DIR/scripts/merge_findings.py final ROUND_DIR` writes `final.md`: kept findings (confirmed and plausible) sorted by severity, alternative directions from kept findings, dropped findings with their reasons, and any finding no verifier reached. If it lists unverified findings or refuses a verdict, delete the refused verdict files, then repeat from 1: merge writes jobs only for what still lacks a verdict.
5. **Root causes**, once per round: read the kept findings and the `class:` lines in their verdicts' notes. When one decision in this change explains several of them, write `ROUND_DIR/findings/root-cause/<n>.md` with `kind: design`, `symptoms:` listing their ids, and the decision under `## Claim`, then `findings/root-cause/_done.md`, and repeat from 1 so a verifier checks it. Skip it when no shared decision shows, and skip a decision a carried root cause already names: add the new symptom ids to that carried finding's `symptoms:` instead and delete its verdict so the repeat verifies it again; if that carried root cause was refuted, write a new root cause.

## 7. Write the review

Write `ROUND_DIR/review.md` from `final.md` and the files it points at, and show it in chat (with `--auto-fix`, show only round 2's):

```markdown
# Review: <PR title or branch>

**Intent:** <what the change does and whether it matches its description>
**Verdict:** <approve | approve with suggestions | changes requested> -- <one-line reason>

## Critical (then High, Medium, Low; omit empty sections)

### <title> -- `path:start-end` · confirmed (<proof>) | plausible (<proof>)

<what's wrong and its impact, 1–3 sentences>
**Evidence:** <the reproduction or checked fact; for plausible, the judgment record's path and what settles it>
**Fix:** <the smallest fix>
**Regression test:** <the verifier's regression test, when present>

A root cause takes the same heading and adds, under its Fix, `Fixing the symptoms alone leaves this open.` and its symptoms as a list: `- <title> -- path · <status> (<proof>): <one line>. Fix: <the smallest fix>`.

## Related open PRs

- #<n> <title> -- <conflict in meaning | dependency | text conflict>: <one line>

## Alternative directions

Broader changes to code this change didn't write. They don't affect the verdict.

- **<title>** (from <finding title>): <the direction, 1–3 sentences>

---

Reviewed for: <concerns covered>. <N> findings dropped in verification (<refuted>, <unproven>, <duplicates>); reasons in ROUND_DIR/final.md. Dropped as unproven at critical or high: <titles, or none>.
```

The verdict follows the kept findings, design findings included: any confirmed critical or high means changes requested; for a plausible critical or high, decide from its traced path and say which way you went in the reason. Otherwise approve with suggestions when anything is kept, and approve when nothing is.

Write in one voice; don't attribute findings to specialists. A finding listed in a root cause's `symptoms` appears only nested under it. Omit the related PRs and alternatives sections when empty.

Post to the PR only when the user asks (asking up front counts as confirmation). Summary comment: `gh pr comment <n> --body-file ROUND_DIR/review.md`. Inline: one PR review via `gh api repos/{owner}/{repo}/pulls/<n>/reviews` with a `comments` array, so it lands as a single review. Never approve or request changes on the PR unless asked.

## 8. Auto-fix

Only with `--auto-fix`, and only from round 1. Fixers commit on top of the reviewed head in round 1's `REVIEW_TREE`, pinned by `FIXES_REF`; nothing is pushed until the user says so.

1. **Ask once, up front.** In one question, put each alternative in `final.md` (take it instead of the fix, fix the finding the quick way, or leave the finding open) and each environment need named under a plausible verdict's `Blocked by`. Skip it when there are neither.
2. **Junior pass.** List the confirmed findings whose verdict has a `## Regression test` and `fix_check: pass`, and whose alternative the user didn't take. If there are any, write `ROUND_DIR/jobs/fixer-junior.md` and launch a `junior` with `You are a code fixer. Read your job file at ROUND_DIR/jobs/fixer-junior.md and follow it.`
3. **Senior pass.** List every other kept finding the user didn't leave open, plus the junior's `handed off` items. If there are any, write `ROUND_DIR/jobs/fixer-senior.md` with the alternatives the user took and their answers from 1, and launch a `senior` with the same prompt pointing at it.
4. **Ask about what's open.** Bring every `needs impact decision`, `blocked`, and `deferred` item in `ROUND_DIR/fixes.md` to the user in one question, using the ask each item ends with.
5. **Senior pass 2.** Send the fixer the answers, or if your harness can't resume it, launch a new one on `ROUND_DIR/jobs/fixer-senior-2.md` with the open findings and the answers. This is the last pass: whatever is still open goes in the review as a named blocker.
6. **Round 2.** If `FIXES_REF` doesn't exist, nothing was fixed: show the user what's open in `fixes.md` and stop. Otherwise run `init_run.sh <target> FIXES_REF` and follow steps 2 to 7 as a re-review, without fixing. Its review says what the fixers fixed (from round 1's `fixes.md`), what is still open and what it needs, and what round 2 found.
7. **Deliver.** The fix sha is `git rev-parse FIXES_REF`. For a PR, offer `git push origin <fix sha>:<headRefName>`, and push only if `git ls-remote origin <headRefName>` still shows the reviewed head. For a local branch, tell the user to run `git merge --ff-only <fix sha>`. For uncommitted work, tell them to run `git diff <reviewed head> <fix sha> | git apply`.

Fixer job file (the junior's has no alternatives or answers):

```markdown
# Job: fixer-junior | fixer-senior | fixer-senior-2

Role: SKILL_DIR/agents/fixer-junior.md or SKILL_DIR/agents/fixer-senior.md
Protocol: SKILL_DIR/references/fix-protocol.md
Format: SKILL_DIR/references/findings-format.md
Brief: ROUND_DIR/brief.md
Code: REVIEW_TREE
Pin: FIXES_REF
Notes: ROUND_DIR/notes/<job>.md
Output: ROUND_DIR/fixes.md

## Findings

- <id>: ROUND_DIR/findings/<job>/<n>.md, ROUND_DIR/verified/<id>.md

## Take the alternative

- <id>

## User answers

- <id>: <answer>
```

## 9. Clean up

`SKILL_DIR/scripts/cleanup_run.sh <target>` removes the review and scratch worktrees of every round, and `FIXES_REF`. Keep the round directories. With `--auto-fix`, clean up only once the user has pushed or merged the fixes, or said to discard them.

## Related open PRs

Write `ROUND_DIR/jobs/related-prs.md` and launch an `associate` on it:

```markdown
# Job: related-prs

Code: REVIEW_TREE (head <head-sha>, PR #<n>)
Output: ROUND_DIR/related-prs.md

Facts only; no verdicts.

Symbols this PR renames, removes, or changes the signature of: <list, or none>

1. List every open PR except #<n>, drafts included, with no limit: `gh pr list --state open --limit 1000 --json number,title,url,author,isDraft,updatedAt,baseRefName,headRefOid,files`. The `files` list stops at 100; for a PR with 100 files, get the full list with `gh pr diff <m> --name-only`.
2. Keep the PRs that change at least one file this PR changes. For each, `git fetch origin pull/<m>/head`, then record: the shared files; per shared file, the base-side line ranges each PR changes (the `-start,count` of each hunk header in `git diff origin/<its base>...<its head>`) and whether they overlap; functions or symbols both PRs change; and `git merge-tree --write-tree --name-only <head-sha> <their head>` (exit 1 means conflict: list the conflicted files).
3. For each symbol listed above, search every other open PR's diff (`gh pr diff <m>`), shared files or not, and record each PR whose added lines still use it, with file and line.
4. Write the output as one `## #<m> <title>` section per PR with url, author, draft, last update, base, and the facts above. Start the file with how many open PRs you checked, how many share files, and how many use a listed symbol. If `gh` or GitHub is unavailable, write one line saying why and stop.
```

When it finishes, judge each PR it lists (a PR that still uses a symbol this PR renames or removes is at least a dependency): **independent**, **text conflict** (merge-tree conflicts, meaning compatible), **conflict in meaning** (merges cleanly or not, but together they behave wrong: both change the same behavior, one relies on what the other changes), or **dependency** (one needs the other merged first). Every verdict except independent becomes a finding in `ROUND_DIR/findings/related-prs/<n>.md` at the overlapping location in this PR, with severity from the rubric and a trigger naming both heads; then write `findings/related-prs/_done.md`. They are verified like every other finding. If the check was skipped, say so in the review footer.

## Re-review

When `PREV_ROUND_DIR` is set, this round reviews what changed since that round's head (in its `round.md`):

- **Already decided.** Add an "Already decided" section to the brief listing every finding from `PREV_ROUND_DIR/final.md`, kept and dropped, with title, location, status, and reason. Specialists never report kept ones again (they are carried, below) and report a dropped one only if its lines changed or they have new evidence.
- **Routing.** If `git merge-base --is-ancestor <previous head> <head-sha>` succeeds, route specialists to the concerns that `git diff <previous head> <head-sha>` touches, with that diff as their scope. If it fails, the branch was rebased or force-pushed and that diff would mix in base changes: route and scope specialists on the full PR diff as in a first round. `hunks.md` covers the full PR diff either way.
- **Carried findings.** Copy every kept finding from `PREV_ROUND_DIR/final.md`, except related-PR ones, into `findings/carried/<n>.md`, with `file` and `lines` moved to where that code sits at this head (keep the old location if the code is gone) and `carried_from: PREV_ROUND_DIR/verified/<id>.md` added to the frontmatter; set `kind` from the format reference when the old finding has none. Finding ids are numbered per round, so for a carried root cause rewrite `symptoms:` to the `carried-<n>` ids its symptoms got, and drop ids whose finding wasn't carried (drop the whole line when none remain); merge refuses a `symptoms:` id that names no finding in this round. Then write `findings/carried/_done.md`. Each is verified again this round, so a carried finding never skips a round. Related-PR findings are judged afresh from this round's `related-prs.md` instead.
- **Review.** Say which carried findings still hold and which the new pushes fixed (those come back refuted with evidence starting `Fixed at`).
