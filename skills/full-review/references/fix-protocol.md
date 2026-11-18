# Fix protocol

Every fixer follows this. Your job file has all the paths; reread it whenever you're unsure where something is.

## Rules

- Work only in the `Code` checkout from your job file. It starts at the reviewed head plus any earlier fixer's commits; commit on top of them. Write only to that checkout, your notes file, and `Output`. Never push.
- Fix only the findings in your job file, each with the smallest change that closes its gap. Honor the repo's conventions files listed in the brief.
- Don't call production services, real external APIs, or real databases. Local stand-ins are fine.

## Each fix

- Prove the fix the way the verdict proved the finding. When the verdict has a `## Regression test`, work red to green with it, through a TDD skill if one is available: add it at `regression_test_target`, see it fail, apply the fix, see it pass, then run the suite. For a test-gap the test is the fix: see it fail with the verdict's mutation applied and pass without it, then run the suite. Otherwise rerun the reproduction from the verdict's `## Evidence`, see it pass after the fix, then run the suite. For a `fact` verdict, recheck the quoted fact after the fix and record in `Output` what now holds. For a `trace` verdict, run its judgment record's Settles it when you can; otherwise walk its Path again after the fix and record in `Output` what now stops it. Add a test of your own only when it passes the regression-test gate in the format reference.
- Commit each finding on its own with a conventional commit message, such as `fix(api): check ownership before PATCH /users/:id`, and no attribution lines. Then run `git update-ref <Pin> HEAD` so the commit survives cleanup.
- To abandon an attempt, `git reset --hard` and `git clean -fd` in your checkout, back to your last commit.

## Recording outcomes

Write each finding's outcome to `Output` as soon as you settle it, in the `fixes.md` format from the format reference. If the file already has an item for that id, replace it. Write just enough for the next agent to pick it up.

## Notes file

Create the notes file named in your job file before you start, with a checklist of your finding ids, and tick each once its outcome is written. After context compaction, or whenever you lose track, reread your job file, notes, and `fixes.md` before continuing.
