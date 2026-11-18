# Specialist protocol

Every specialist follows this. Your job file has the paths; reread it whenever you're unsure where something is. `findings-format.md` and `severity-rubric.md` sit beside this file.

## Rules

- **Read-only.** Read code from the `Code` path in your job file. Never edit, stage, commit, or post anything. Write only to your notes file and your `Output` directory.
- **Own your concern across your whole scope**, however many files that is. Stay out of other specialists' concerns (listed in your job file). If you notice something serious outside yours, add a one-line handoff and move on.
- **Don't repeat CI or linter results** listed in the brief.
- **Honor the repo's conventions files** listed in the brief: something they explicitly allow is not a finding.
- **Earlier review.** When the brief names one, read it first and report each of its open findings in your concern that still holds at this head.

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

**Evidence bar.** A finding names specific lines and a concrete trigger: the input, state, or call path that makes it go wrong, and what happens then. "This could be a problem" without a path is a signal for your notes, not a finding. A separate verifier proves every finding by a test, a reproduction, or a checked fact, and keeps one that nothing can run as plausible. So report what you can argue from the code even when you see no way to execute it. Precision matters more than volume; finding nothing in your area is a valid, useful result.

**Kind.** Every finding sets `kind` from the format reference, chosen by how it goes wrong rather than by your specialty: a security hole that needs a race is a `scenario`.

**Fix, why, and alternative.** `## Fix` is always the smallest change that closes the gap. Before writing it, ask why once more: what made this mistake possible? When the answer is a decision this change makes (a new boundary, data shape, format, protocol, or dependency), report that decision as its own `kind: design` finding, naming the failures it causes under `## Claim`. When the answer is a decision that predates this change (the test setup can't catch it, a framework should be replaced), keep the smallest fix and add the broader direction under `## Alternative`. Severity rates the gap, never the size of the alternative. Add at most 2 alternatives per job.

**Limits.** Report every critical and high finding. Report at most 5 medium or low findings, the ones that matter most.

**Escalate instead of guessing.** If a hunk needs deeper analysis than you can give with confidence, list it under escalations in your done file with a one-line reason rather than reporting a weak finding or staying silent.

## Finish

Check your coverage against your scope. Then write `_done.md` in your `Output` directory, last: escalations, handoffs, and anything you didn't review under uncovered.
