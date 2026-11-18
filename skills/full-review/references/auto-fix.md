# Auto-fix rounds

With `--auto-fix` a run is up to 5 rounds. Round 1 is the review you just verified. Each later round reviews only what the fixers changed. The run asks the user nothing: whatever needs their decision ends as an open item in the review.

## After each round's step 4

1. In round 5, go to Finish. Otherwise run `python3 SKILL_DIR/scripts/merge_findings.py fix ROUND_DIR --review-tree REVIEW_TREE --fixes-ref FIXES_REF`, adding `--min-severity high` from round 2 on, so later rounds fix only critical and high findings. It writes the fixer job files. When it prints `0 to fix`, go to Finish.
2. Launch a `junior` on `ROUND_DIR/jobs/fixer-junior.md` when the script wrote it. When it returns, launch a `senior` on `ROUND_DIR/jobs/fixer-senior.md` when that file lists a finding or `ROUND_DIR/fixes.md` has a `handed off` item. They share one checkout, so run them one after the other.
3. When `git rev-parse FIXES_REF` is what it was before step 2, or the ref doesn't exist, nothing was fixed: go to Finish.
4. Start the next round: `SKILL_DIR/scripts/init_run.sh <target> FIXES_REF`, then steps 2 to 4 of the skill with the previous round's head as base and the fix sha as head. The intent is the previous round's `fixes.md`: each commit should close the finding it names and change nothing else. Leave out the related-PRs check and the earlier review. A fix diff is usually small enough to review solo.

## Finish

1. `python3 SKILL_DIR/scripts/merge_findings.py report <this run's round dirs, in order>` writes `report.md` in the last round dir: what was fixed, what is open, the alternatives, and the dropped counts.
2. Write `review.md` in the last round dir from `report.md`, in the step 5 shape with these changes:
   - Add `## Fixed` after the verdict, one line per fixed finding with its commit.
   - The severity sections hold the open findings; read each one's finding and verdict files. When an open line carries a fixer's note, add `**Needs:**` with what the user must decide or provide.
   - The verdict follows the open findings.
   - The footer points at each round's `final.md`.
3. Deliver the fixes. The fix sha is `git rev-parse FIXES_REF`.
   - **PR:** when `git ls-remote origin <headRefName>` still shows the head round 1 reviewed, `git push origin <fix sha>:refs/heads/<headRefName>`. When the branch moved, leave it and give the user the fix sha.
   - **Local branch:** tell the user to run `git merge --ff-only <fix sha>`.
   - **Uncommitted work:** tell the user to run `git diff <reviewed head> <fix sha> | git apply`.
4. Clean up (step 6) when the fixes are pushed or there were none. Otherwise `FIXES_REF` is all that keeps the fix commits: leave it, and give the user the cleanup command to run once they have applied them.
