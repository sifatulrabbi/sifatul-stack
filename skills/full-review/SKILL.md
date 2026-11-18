---
name: full-review
description: Multi-agent review of a PR or diff -- specialist reviewers chosen by risk, every finding proved by a separate verifier, one consolidated review; --auto-fix also fixes what it finds. Use when the user asks to review or audit code changes, or to check them before merging.
---

# Full review -- lead

You are the lead. You check the change against its intent, decide who reviews it, get every finding verified, and write the one review. You alone talk to the user and post. Every other agent reads code from the round's review tree and writes only inside the round directory.

Launch agents by these names, never by model; if your harness lacks one, use its closest model:

- `senior`: you, the specialists, the verifiers for findings a plain test can't prove, and the senior fixer.
- `junior`: the verifiers for runnable findings, and the junior fixer.
- `associate`: the scout and the related-PRs check.

Every agent gets a job file at `ROUND_DIR/jobs/<job>.md` and the prompt `Read your job file at <path> and follow it.` The scripts write the verifier and fixer job files. You write the others in this shape, leaving out the lines a role has no use for:

```markdown
# Job: <job>

Role: SKILL_DIR/agents/<role>.md
Protocol: SKILL_DIR/references/specialist-protocol.md
Brief: ROUND_DIR/brief.md
Hunks: ROUND_DIR/hunks.md
Code: REVIEW_TREE
Context: ROUND_DIR/context.md
Notes: ROUND_DIR/notes/<job>.md
Output: <where this job writes>
Scope: <files, with hunk ranges when only part of a file belongs to this job>
Other specialists in this run: <names>
Focus: <the risk-map items for this job>
```

`SKILL_DIR` is this skill's directory. The other paths come from step 1.

With `--auto-fix`, read `SKILL_DIR/references/auto-fix.md` when step 4 ends and follow it before step 5.

## 0. Fresh session

A review is cold only when its reviewer didn't write the code. If this session edited, committed, planned, or implemented any of the code under review, stop and tell the user: `Start a new session in this repo and run full-review on <target>.`

## 1. Set up the round

The target is always a diff. Identify its base and head, leaving the user's checkout as it is:

- **GitHub PR:** `gh pr view <n> --json number,title,body,url,baseRefName,headRefName,headRefOid`, then `git fetch origin <baseRefName> pull/<n>/head`. Base is `origin/<baseRefName>`, the PR's own base branch. Target: `pr/<n>`.
- **Local branch:** base is the default branch, head is the branch tip. Target: `branch/<name>`.
- **Uncommitted work:** head is `git stash create`, or `HEAD` when that prints nothing. It leaves out untracked files, so ask the user to stage any that belong in the review. Target: `wip/<branch>`, or `wip/HEAD` on a detached HEAD.

Run `SKILL_DIR/scripts/init_run.sh <target> <head-sha>` from the repo root. It prints `ROUND_DIR` (all of the round's files), `REVIEW_TREE` (a detached worktree at head), `WORKTREE_BASE` (where verifier scratch worktrees go), `FIXES_REF` (pins fixer commits), and `PREV_ROUND_DIR` (the last finished review of this target, or empty).

```
ROUND_DIR/
  brief.md  hunks.md  context.md  related-prs.md
  jobs/<job>.md            # one per launched agent
  notes/<job>.md           # each agent's working memory
  findings/<job>/<n>.md    # one per finding, then _done.md
  verified/<id>.md         # one verdict per finding
  final.md  review.md
```

## 2. Triage

1. `python3 SKILL_DIR/scripts/classify_hunks.py --base <base> --head <head-sha> -o ROUND_DIR/hunks.md` labels each hunk mechanical or semantic. Skim the mechanical section and treat anything that isn't really mechanical as semantic. Only semantic hunks are reviewed.
2. Launch two `associate`s and keep working while they run:
   - **scout** (role `scout.md`, output `ROUND_DIR/context.md`), every round.
   - **related-prs** (role `related-prs.md`, output `ROUND_DIR/related-prs.md`), for a GitHub PR. Add two lines to its job file: `PR: #<n> at <head-sha>` and `Symbols: <what this PR renames, removes, or changes the signature of, or none>`.
3. Read the PR title and description, the linked issue, CI results (`gh pr checks <n>`), and the semantic diff.
4. **Intent check:** does the diff do what it claims, and only that? When the intent clearly doesn't match the diff, or the PR bundles independent changes, stop: tell the user what you found, propose a split, and continue only on their word. Write each smaller missing piece (a claimed behavior the diff doesn't deliver, a caller or migration it needs and lacks) as a finding in `ROUND_DIR/findings/lead/`.
5. **Risk map:** which parts of the change carry which risks (auth, data, public API, concurrency, rollout…).
6. **Decision inventory:** the decisions this change makes that are costly to reverse: a new module boundary, data shape, wire or file format, protocol, dependency, or concurrency model. One line each with its location, or `none`.
7. Write `ROUND_DIR/brief.md`: base and head, intent (claimed vs actual), risk map, decision inventory, the repo's convention files (REVIEW.md, CLAUDE.md, CONTRIBUTING.md, when present), what CI and linters already reported, and what's out of scope. When `PREV_ROUND_DIR` is set, name `PREV_ROUND_DIR/review.md` as the earlier review.

## 3. Find

Size is semantic surface and risk, never file count: 100 files of renames is small, three files rewriting auth is not.

- **Solo**, for a small, low-risk surface with an empty decision inventory: review it yourself following `references/specialist-protocol.md`, working through the "How to work" sections of these roles in order: architecture, correctness, instructions (when the change touches text agents or people follow), tests. Write findings to `ROUND_DIR/findings/lead/`.
- **Team**, for everything else: one job per concern, each owning its concern across every file it touches. Launch in parallel every specialist whose trigger matches the risk map, once `context.md` exists, with output `ROUND_DIR/findings/<job>/`.

Launch each specialist when the semantic change touches:

- **correctness**: any logic (nearly every team review)
- **security**: auth, permissions, input parsing, external requests, file paths, secrets, crypto, user data, queries built from input
- **architecture**: new modules, services or classes; cross-layer imports; moved responsibilities; a new pattern; any item in the decision inventory
- **concurrency**: async code, threads, locks, queues, shared mutable state, retries, timeouts
- **data-migrations**: schema files, migrations, backfills, ORM models
- **tests**: any behavior change, or test files
- **maintainability**: substantial new or rewritten code
- **performance**: DB queries, loops over collections, request hot paths, caching, batch jobs
- **api-compat**: public APIs, exported functions, request/response schemas, events, CLI flags, config formats
- **dependencies**: manifests or lockfiles
- **ops-rollout**: config, feature flags, logging, metrics, deploy files, anything hard to roll back
- **instructions**: prompts, skills, agent roles, AGENTS.md or CLAUDE.md, runbooks

For a concern no specialist covers (billing math, an ML pipeline), fill in `references/adhoc-specialist.md` as `ROUND_DIR/jobs/<job>-role.md` and launch it the same way.

A job is finished when its `findings/<job>/_done.md` exists; write `findings/lead/_done.md` yourself once your own findings are in. Read each specialist's `_done.md` and launch a follow-up job with the same role for the scope it lists under Uncovered or Escalations, and a new specialist for a Handoff no running job covers.

**Related PRs.** Judge each PR in `related-prs.md` as independent, a **text conflict** (they conflict on merge but mean compatible things), a **conflict in meaning** (together they behave wrong, whether or not they merge cleanly), or a **dependency** (one needs the other merged first; a PR still using a symbol this one renames or removes is at least that). Write every verdict except independent as a finding in `ROUND_DIR/findings/related-prs/<n>.md` at the overlapping location in this PR, with no `kind`, severity from the rubric, and a trigger naming both heads. Then write `findings/related-prs/_done.md`.

## 4. Verify

A separate agent proves every finding, so the user sees only findings that survived.

1. `python3 SKILL_DIR/scripts/merge_findings.py merge ROUND_DIR --review-tree REVIEW_TREE --worktree-base WORKTREE_BASE` writes the verifier jobs and prints each as `verify-<n>: <agent>`. When it refuses, it names the file: have the agent that wrote it finish or fix it, or fix it yourself when it is yours.
2. For each printed job: `git worktree add --detach WORKTREE_BASE/verify-<n> <head-sha>`, then launch it on the agent printed beside it. Launch them all in parallel.
3. `python3 SKILL_DIR/scripts/merge_findings.py final ROUND_DIR` writes `final.md` and prints the counts. While it prints unverified findings, repeat from 1: those are findings a junior escalated or no verifier reached, and merge writes jobs only for them. When it refuses a verdict, delete that verdict file first.

## 5. Write the review

Write `ROUND_DIR/review.md` from `final.md` and the finding and verdict files it lists, and show it in chat:

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

## Related open PRs

- #<n> <title> -- <conflict in meaning | dependency | text conflict>: <one line>

## Alternative directions

Broader changes to code this change didn't write. They don't affect the verdict.

- **<title>** (from <finding title>): <the direction, 1–3 sentences>

---

Reviewed for: <concerns covered>. <N> findings dropped in verification (<refuted>, <unproven>, <duplicates>); reasons in ROUND_DIR/final.md. Dropped as unproven at critical or high: <titles, or none>.
```

- **Verdict:** any confirmed critical or high means changes requested. For a plausible critical or high, decide from its traced path and say which way you went. Otherwise approve with suggestions when anything is kept, and approve when nothing is.
- **Shared cause:** when one decision in this change explains several kept findings, put them under one heading that names the decision, at their highest severity, and say that fixing them one by one leaves it open.
- Write in one voice, with findings unattributed. Related-PR findings go in their own section. Omit an empty section, and say in the footer when the related-PRs check didn't run.

Post to the PR only when the user asked for it: `gh pr comment <n> --body-file ROUND_DIR/review.md`, or for inline comments one PR review via `gh api repos/{owner}/{repo}/pulls/<n>/reviews` with a `comments` array. Leave the PR's approval state alone unless asked to set it.

## 6. Clean up

`SKILL_DIR/scripts/cleanup_run.sh <target>` removes the review and scratch worktrees and `FIXES_REF`, and keeps the round directories.
