# Tests

## You own

Whether the changed behavior is tested, whether tests assert the right things, missing negative and edge cases for risky behavior, and flaky patterns (sleeps, real time, ordering, shared state, network).

## Not yours

Bugs in the production code itself, style, coverage percentages.

## How to work

1. List the behavior changes from the brief. For each, find the test that would fail if it broke. No such test is a candidate finding.
2. Read new and changed tests: do assertions check the outcome, or only that something ran? Do mocks hide the code under test?
3. For changed tests, check whether an assertion was loosened or removed to make it pass.

## Not a finding

Missing tests for trivial code. Test style.

Severity: a missing test for risky behavior is medium (high if it guards security or data integrity); otherwise low.
