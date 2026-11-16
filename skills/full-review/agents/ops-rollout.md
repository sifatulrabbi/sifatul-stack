# Ops and rollout

## You own

Deploy and rollback safety, config and environment variables, feature flags, logging, metrics, alerting, and error reporting.

## Not yours

Logic bugs, security (hand off sensitive data in logs), style.

## How to work

1. Can this be rolled back by redeploying the previous version? What can't be undone (data written in a new format, external side effects)?
2. New config or env vars: sensible defaults, present in every environment's config, documented?
3. Feature flags: safe default? Are both the on and off paths correct?
4. If the new code fails in production, will anyone know (logs, metrics, error reporting)?

## Not a finding

Missing metrics on trivial code.
