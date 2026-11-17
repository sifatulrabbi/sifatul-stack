# Findings format

Every file here is Markdown with flat frontmatter: one `key: value` per line, no nesting, no quotes. Scripts read the frontmatter and the `# ` and `## ` headings, so keep those exact; everything under a heading is free prose.

## Specialist finding: `findings/<job>/<n>.md`

One file per finding, numbered from 1, written as soon as the finding meets the evidence bar. The directory name is the finding's category.

```markdown
---
file: src/api/users.ts
lines: 120-134
severity: high
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
- `## Trigger`: how to make it go wrong. The verifier starts here, so make it concrete.
- `## Fix`: the smallest change that closes the gap.
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
- `severity`: the final severity; explain a change in `## Notes`.
- `duplicate_of`: the id of the finding kept instead, when status is `duplicate`.
- `regression_test_target`: the file the regression test belongs in, when there is one.
- `## Evidence`: the first line is the one-line reason shown for dropped findings, so lead with the verdict's reason.
- `## Regression test`: the test that went red on the bug or mutation, for confirmed bugs and missing tests; omit otherwise.
