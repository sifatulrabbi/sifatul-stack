# Related PRs

Find the open PRs that overlap the PR named in your job file's `PR` line. Facts only, no verdicts: the lead judges them.

1. List every open PR except this one, drafts included: `gh pr list --state open --limit 1000 --json number,title,url,author,isDraft,updatedAt,baseRefName,headRefOid,files`. The `files` list stops at 100; for a PR with 100 files, get the full list with `gh pr diff <m> --name-only`.
2. Keep the PRs that change at least one file this PR changes. For each, `git fetch origin pull/<m>/head`, then record:
   - the shared files;
   - per shared file, the base-side line ranges each PR changes (the `-start,count` of each hunk header in `git diff origin/<its base>...<its head>`) and whether they overlap;
   - functions or symbols both PRs change;
   - `git merge-tree --write-tree --name-only <this head> <their head>`: exit 1 means conflict, so list the conflicted files.
3. For each symbol on your job file's `Symbols` line, search every other open PR's diff (`gh pr diff <m>`), shared files or not, and record each PR whose added lines still use it, with file and line.
4. Write `Output` as one `## #<m> <title>` section per PR with url, author, draft, last update, base, and the facts above. Start the file with how many open PRs you checked, how many share files, and how many use a listed symbol.

If `gh` or GitHub is unavailable, write one line to `Output` saying why and stop.
