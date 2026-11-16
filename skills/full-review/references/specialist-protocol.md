# Specialist protocol

Every specialist follows this. Your job file has all the paths; reread it whenever you're unsure where something is.

## Rules

- **Read-only.** Read code from the `Code` path in your job file. Never edit, stage, commit, or post anything. Write only to your notes and output files.
- **Own your concern across your whole scope**, however many files that is. Stay out of other specialists' concerns (listed in your job file). If you notice something serious outside yours, add a one-line handoff and move on.
- **Don't repeat CI or linter results** listed in the brief.
- **Honor the repo's conventions files** listed in the brief: something they explicitly allow is not a finding.

## Notes file

Create your notes file before reading code and keep it current. It's your memory: after context compaction, or whenever you lose track, reread your job file and notes before continuing.

```markdown
## Coverage

- [x] src/api/users.ts (all hunks)
- [ ] src/api/orders.ts

## Signals

- users.ts:88 token compared with ==, check whether timing matters here

## Draft findings

- users.ts:120-134 PATCH has no ownership check (high), trigger: …
```

Update coverage after each file or group of hunks, and write draft findings as soon as you have them.

## Evidence bar

A finding names specific lines and a concrete trigger: the input, state, or call path that makes it go wrong, and what happens then. "This could be a problem" without a path is a signal for your notes, not a finding.

Every finding is independently verified by someone trying to reproduce it, and findings that can't be reproduced are dropped. Precision matters more than volume. Finding nothing in your area is a valid, useful result; don't stretch to fill the output.

## Limits

Report every critical and high finding. Report at most 5 medium or low findings, the ones that matter most.

## Escalate instead of guessing

If a hunk needs deeper analysis than you can give with confidence, add it to `escalations` with a one-line reason rather than reporting a weak finding or staying silent.

## Finish

Check your coverage against your scope; anything you didn't review goes in `uncovered`. Then write your output file in the format from the schema.
