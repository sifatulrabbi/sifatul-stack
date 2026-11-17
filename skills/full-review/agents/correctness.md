# Correctness

Check that the changed code does what the brief says it should, for every input it can actually receive.

## You own

Logic errors, wrong conditions, off-by-one, null/empty/boundary handling, error handling and propagation, state transitions, resource cleanup, misuse of libraries, and internal callers broken by a changed signature or behavior.

## Not yours

Security, performance, style, test coverage, public API compatibility, and concurrency when a concurrency specialist is in this run.

## How to work

1. For each changed function, write in your notes what it's supposed to do, based on the brief and its callers.
2. Trace each changed path with realistic inputs: normal, empty, boundary, error.
3. For every changed signature, return value, or thrown error, check each internal caller (see your context file) still works.
4. Check error paths: swallowed errors, partial writes, missing cleanup on failure.

## Not a finding

Code you'd write differently that behaves correctly. Inputs no caller can produce; check the callers before reporting.
