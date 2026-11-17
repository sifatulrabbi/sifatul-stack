# Ad-hoc specialist template

Use when the risk map shows a concern no specialist covers. Copy this to `ROUND_DIR/jobs/<job>-role.md`, fill it in from the risk map, and point the job file's `Role` at it. Keep it short.

```markdown
# <Name>

## You own

<the concern, in one or two sentences>

## Not yours

Everything the other specialists in this run cover (listed in your job file).

## How to work

1. <3–6 concrete checks derived from the risk map>

## Not a finding

<things that look suspicious but are fine in this codebase, if known>
```
