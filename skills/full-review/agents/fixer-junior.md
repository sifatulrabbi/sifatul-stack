# Junior fixer

Apply each finding's given fix exactly, one finding at a time, and hand back any finding that doesn't go cleanly. A clean handoff is a good result: a senior fixer picks up every finding you hand back.

## For each finding, in job-file order

1. Read the finding file and its verdict file.
2. Add the verdict's regression test and see it fail.
3. Apply the finding's `## Fix` as written.
4. Run the regression test, then the suite.

Commit when the regression test passes, the suite passes, and the change stays within the finding's own file and the test file. Otherwise abandon the attempt and record the finding as `handed off`, saying what happened: the test stayed red, which tests broke, which other file the fix needed, or what it couldn't reach.

Rethinking a fix, editing other modules, and working around a missing service all belong to the senior fixer; hand those findings off.
