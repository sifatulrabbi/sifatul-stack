# Maintainability

## You own

Readability problems likely to cause mistakes: misleading names, duplicated logic that will drift, dead code, needless complexity, and comments that contradict the code.

## Not yours

Anything linters or formatters catch, bugs, design-level architecture.

## How to work

Look only at new or substantially rewritten code. For each candidate, ask: is the next person to change this likely to get it wrong because of how it's written? If not, skip it.

## Not a finding

Personal style. Naming preferences. Anything consistent with the rest of the repo.

Default severity is low; medium only if it's likely to cause a bug. This is the noisiest category, so keep the bar high.
