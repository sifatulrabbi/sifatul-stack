# Findings format

Every file here is Markdown with flat frontmatter: one `key: value` per line, no nesting, no quotes. Scripts read the frontmatter and the `# ` and `## ` headings, so keep those exact; everything under a heading is free prose.

## Specialist finding: `findings/<job>/<n>.md`

One file per finding, numbered from 1, written as soon as the finding meets the evidence bar. The directory name is the finding's category.

```markdown
---
file: src/api/users.ts
lines: 120-134
severity: high
kind: behavior
---

# PATCH /users/:id has no ownership check

## Claim
Any authenticated user can update another user's profile.

## Trigger
As user A, send PATCH /users/<B's id> with a new email. The handler updates B without comparing req.user.id to the id param.

## Evidence
users.ts:126 loads by params.id and saves at :133 with no authz call between. Sibling handlers call requireOwner() (orders.ts:41).

## Fix
Call requireOwner(req, params.id) before loading the user.

## Alternative
Move ownership checks into a route-level middleware so new handlers get them by default.
```

- `lines`: head-side line numbers in the code tree, `start-end` or a single line. For a PR-wide finding, leave `file` and `lines` empty.
- `severity`: `critical`, `high`, `medium`, or `low`, from the severity rubric.
- `kind`: how it goes wrong, which decides how the verifier proves it. Related-PR findings leave it out.
  - `behavior`: a deterministic wrong result for some input or call.
  - `scenario`: wrong only under an ordering, timing, failure, old data, or environment.
  - `test-gap`: a missing test, or a test that doesn't earn its keep.
  - `design`: a decision in this change (a boundary, data shape, format, protocol, dependency) that causes or invites failures.
  - `instructions`: text an agent or person follows (a prompt, skill, agent role, runbook) that will lead them wrong.
- `symptoms` (root causes only): the comma-separated ids of the kept findings this design finding causes, all from the same round. Merge refuses an id that names no finding in the round or names another root cause.
- `## Trigger`: how to make it go wrong. The verifier starts here, so make it concrete. For a scenario, specify the reproduction setup: the starting state, the ordering, timing, or fault, and how to force it.
- `## Fix`: the smallest change that closes the gap.
- `carried_from` (carried findings only): the previous round's verdict file for this finding, which the verifier reruns.
- `## Alternative` (optional): the bigger root-cause direction, when the real gap deserves more than the smallest fix (a redesign, a new layer, replacing a framework). It carries no severity and reaches the review only if its finding survives verification.

## Specialist done file: `findings/<job>/_done.md`

Written last. Its presence tells the lead the job finished; the merge script refuses to run without it. Omit empty sections.

```markdown
# Done

## Escalations
- src/auth/session.ts:40-75: token refresh logic too involved to judge confidently

## Handoffs
- src/api/users.ts:200-210 → performance: query inside a loop over all users

## Uncovered
- src/api/orders.ts: ran out of budget before reaching it
```

## Verifier result: `verified/<id>.md`

One file per finding id from the job file.

````markdown
---
status: confirmed
severity: high
proof: test
fix_check: pass
regression_test_target: test/api/users.test.ts
---

## Evidence
PATCH /users/2 as user 1 returned 200 and changed user 2's email (test below, run with `npm test -- users`).

## Regression test
```ts
it("rejects PATCH on another user", async () => {
  const res = await as(user1).patch("/users/2").send({ email: "x@y.z" });
  expect(res.status).toBe(403);
});
```

## Notes
````

- `status`: `confirmed` | `plausible` | `unproven` | `refuted` | `duplicate`. Only confirmed and plausible reach the user.
- `proof` (confirmed and plausible): `test` | `run` | `fact` | `trace`, as defined in the verifier role.
- `fix_check`: `pass` when the finding's `## Fix` turned the reproduction green, `fail` when it didn't; omit without a reproduction.
- `severity`: the final severity; explain a change in `## Notes`.
- `duplicate_of`: the id of the finding kept instead, when status is `duplicate`.
- `regression_test_target`: the file the regression test belongs in, when there is one.
- `alternative: drop`: the finding's `## Alternative` makes a false factual claim; it stays out of the review.
- `## Evidence`: the first line is the one-line reason shown for dropped findings, so lead with the verdict's reason.
- `## Judgment` (plausible only): Path, Guards searched, Blocked by, Settles it, one line each.
- `## Regression test`: a test that went red on the bug or mutation, only when it passes the gate below; omit otherwise.
- `## Notes`: severity changes, `fix_check: fail` reasons, and the sibling probe's `class:` line.

### Regression-test gate

A test earns a place in the repo, here or in a fix, when it:

- checks observable behavior (an output, a state, a status), never the wording of a message, prompt, or doc;
- is simpler than the code it guards;
- survives a harmless refactor or rewording.

Changes to prompts, skills, and docs never carry one; the only test such a change can need guards code that parses the text.

## Fix outcomes: `fixes.md`

One list item per finding the fixers took, in the order they were settled. No frontmatter.

```markdown
# Fixes

- security-1: fixed in a1b2c3d. requireOwner before load; regression test added.
- correctness-3: fixed in d4e5f6a. Also updated billing/invoice.ts and its test, which asserted the old rounding.
- tests-4: handed off. The fix also needs src/jobs/retry.ts.
- api-1: needs impact decision, commit 9f8e7d6. GET /orders `total` becomes a number; web and mobile clients read it as a string.
- data-1: blocked. Proving the fix needs Postgres 15; run `docker compose up db` or give a DSN.
- architecture-1: deferred. The fix becomes moving authz into middleware; scope call needed.
```

- Outcomes: `fixed in <sha>`, `handed off` (junior fixer only), `needs impact decision`, `blocked`, `deferred`.
- Every open item ends with what the user must decide or provide.
