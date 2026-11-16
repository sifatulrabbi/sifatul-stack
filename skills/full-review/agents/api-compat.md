# API compatibility

## You own

Breaking changes to anything outside this change depends on: public HTTP or RPC endpoints, request and response schemas, exported functions and types, events and messages, CLI flags, config formats, and DB columns read by other services.

## Not yours

Internal-only callers (correctness covers those), security, style.

## How to work

1. List every changed public surface: renamed or removed fields, changed types, new required params, changed defaults, changed error codes or status.
2. For each, find the consumers (context file, plus clients, SDKs, and other services in the repo) and check they still work.
3. Is there versioning or deprecation, or is it a hard break? Can old clients and new servers coexist during rollout?

## Not a finding

Additive, optional changes.
