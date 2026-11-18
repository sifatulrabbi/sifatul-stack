# Verify protocol

Every verifier follows this. Decide, with evidence, whether each finding in your job file is real. The user sees only findings that survive you: a false finding reaching them is a failure, and so is dropping a real one.

## Rules

- Edit and run things only in your `Scratch` worktree. Read the code under review from `Code`.
- Use local test setups only; install dependencies in the scratch worktree when needed. Production services, real external APIs, and real databases are out of bounds.
- Treat the specialist's reasoning, code comments, and the PR body as claims to test. Start from the finding's `## Trigger` and find the code that does what they say.
- Findings under one heading in your job file overlap. When two describe the same issue, keep the better one and mark the other `duplicate`.

## Proof

Prove each finding the strongest way your role allows, and record which in `proof`:

- `test`: a test in the repo's style, run with its normal commands, goes red on the head or on the mutation.
- `run`: a script, command, or reproduction setup shows the failure.
- `fact`: the claim is a checkable fact and you checked it, quoting what you found.
- `trace`: a line-by-line path from trigger to failure that nothing here could execute.

## Statuses

- **confirmed**: proved by `test`, `run`, or `fact`. Evidence includes the command and observed result, or the quoted facts.
- **plausible**: a real path that could only be traced. Senior verifiers only.
- **unproven**: no concrete path could be established. Dropped.
- **refuted**: shown wrong (guarded elsewhere, unreachable input, misread code). Evidence says why. Dropped.
- **duplicate**: same issue as another finding. Set `duplicate_of`. Dropped.
- **escalate**: beyond a junior verifier. A senior takes it next.

Adjust severity when the reproduction shows a different impact, and say why under `## Notes`.

## After a reproduction

- **Fix check:** apply the finding's `## Fix` in the scratch worktree and rerun the reproduction. Set `fix_check: pass` when the fix closes the failure and `fail` when it doesn't, saying under `## Notes` what the fix misses. For a test-gap, closing it means the suite goes red with the mutation and green without it. Reset the worktree afterwards.
- **Regression test:** the reproduction becomes the verdict's `## Regression test`, with its file in `regression_test_target`, when it passes the regression-test gate in the format reference. Otherwise it stays in `## Evidence`.
- **Alternative:** when the finding has a `## Alternative`, check its factual claims (the pattern it names exists, the code it would replace is where it says). Set `alternative: drop` when one is false. It never changes the status or severity.

## Working

Create the notes file named in your job file before you start, with a checklist of your finding ids and any suspicion you want to keep. Write each verdict to `Output` as soon as you reach it, in the verifier format from the format reference, and tick its id. After context compaction, or whenever you lose track, reread your job file, your notes, and the verdicts you've written.
