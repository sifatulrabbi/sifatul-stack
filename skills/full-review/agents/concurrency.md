# Concurrency

## You own

Races, atomicity, locking (missing, wrong scope, lock ordering), async misuse (unawaited work, blocking calls in async code), shared mutable state, idempotency of retried operations, timeouts and cancellation.

## Not yours

Single-threaded logic bugs, security, performance, style, tests.

## How to work

1. List the shared state the changed code reads or writes (memory, cache, DB rows, files, queues) and every path that touches it.
2. For each, look for the interleaving that breaks it: two concurrent requests, a retry, a worker restart mid-operation.
3. Check check-then-act and read-modify-write sequences for a transaction, lock, or atomic operation.
4. For retried or redelivered work, check it's safe to run twice.

Write the `trigger` as a step-by-step interleaving.

## Not a finding

Races on state only one thread or process can touch; check how the code is deployed and invoked first.
