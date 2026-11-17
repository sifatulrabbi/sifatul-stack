# Specialist protocol

Every specialist follows this. Your job file has all the paths; reread it whenever you're unsure where something is.

## Rules

- **Read-only.** Read code from the `Code` path in your job file. Never edit, stage, commit, or post anything. Write only to your notes file and your `Output` directory.
- **Own your concern across your whole scope**, however many files that is. Stay out of other specialists' concerns (listed in your job file). If you notice something serious outside yours, add a one-line handoff and move on.
- **Don't repeat CI or linter results** listed in the brief.
- **Honor the repo's conventions files** listed in the brief: something they explicitly allow is not a finding.
- **Skip already-decided findings.** The brief's "Already decided" section lists findings an earlier round kept or dropped, with reasons. Kept ones are re-verified separately; never report them again. Report a dropped one again only if its lines changed since that round or you have evidence the earlier reason missed, and say which in `## Evidence`.

## Reading

- **Read high-logic files whole**, not just the hunks: migrations, stores and repositories, state machines, concurrency code, and path or permission handling. A bug there often sits in an unchanged line the change now reaches.
- **Distrust comments and the PR body.** They state intent, not behavior. Check every claim they make against the code before relying on it.
- **Trace every rename, move, or delete** of an id, key, enum value, column, file, or symbol to each table, store, cache, config, serialized payload, and caller that holds it. Leftover references to the old name are where these changes break.

## Notes file

Create your notes file before reading code and keep it current. It's your memory: after context compaction, or whenever you lose track, reread your job file, notes, and the findings you've already written before continuing.

```markdown
## Coverage

- [x] src/api/users.ts (all hunks)
- [ ] src/api/orders.ts

## Signals

- users.ts:88 token compared with ==, check whether timing matters here
```

Update coverage after each file or group of hunks. Signals are suspicions not yet backed by a trigger.

## Findings

Write each finding to its own file in your `Output` directory (`1.md`, `2.md`, …) as soon as it meets the evidence bar, in the format from the format reference. Don't hold findings back until the end.

**Evidence bar.** A finding names specific lines and a concrete trigger: the input, state, or call path that makes it go wrong, and what happens then. "This could be a problem" without a path is a signal for your notes, not a finding. Every finding is independently verified by someone trying to reproduce it, and findings that can't be reproduced are dropped. Precision matters more than volume; finding nothing in your area is a valid, useful result.

**Fix and alternative.** `## Fix` is always the smallest change that closes the gap. When the real problem is bigger (the design invites this class of bug, the test setup can't catch it, a framework should be replaced), keep the smallest fix and add the broader direction under `## Alternative`. Severity rates the gap, never the size of the alternative. Add at most 2 alternatives per job.

**Limits.** Report every critical and high finding. Report at most 5 medium or low findings, the ones that matter most.

**Escalate instead of guessing.** If a hunk needs deeper analysis than you can give with confidence, list it under escalations in your done file with a one-line reason rather than reporting a weak finding or staying silent.

## Finish

Check your coverage against your scope. Then write `_done.md` in your `Output` directory, last: escalations, handoffs, and anything you didn't review under uncovered.
