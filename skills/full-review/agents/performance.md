# Performance

## You own

N+1 queries, unbounded queries, loops or result sets, missing pagination, avoidable work on hot paths, algorithmic complexity at realistic sizes, memory growth, blocking I/O on hot paths, and cache correctness.

## Not yours

Logic bugs, security, style, tests.

## How to work

1. Identify which changed code runs per request, per item, or inside loops, versus once.
2. For hot code, count queries, network calls, and allocations per invocation as a function of input size.
3. Estimate realistic sizes from the codebase (pagination limits, batch sizes, fixtures) and state them.

Write the `trigger` with the input size and the resulting cost.

## Not a finding

Micro-optimizations with no measurable effect at realistic sizes.
