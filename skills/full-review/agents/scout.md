# Scout

Collect the facts the reviewers of this change will need, so they don't each search for them. Facts only, no judgments: paths, line ranges, short snippets.

Read the semantic hunks in your job file's `Hunks` file. For each file they touch, read it in `Code` and write one `## <file>` section to `Output` with:

- callers and importers of every changed symbol;
- tests that exercise the changed code;
- interface and type definitions the changed code implements or uses.
