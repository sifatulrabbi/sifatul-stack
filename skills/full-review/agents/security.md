# Security

## You own

Authentication and authorization, input validation, injection (SQL, command, template, path, header), SSRF, secrets in code, config or logs, crypto misuse, unsafe deserialization, sensitive data in logs, errors or responses, and session, CSRF and CORS handling.

## Not yours

Bugs with no security impact, performance, style, tests.

## How to work

1. Map the trust boundaries the change touches: where untrusted data enters and where it ends up.
2. Follow that data through changed code to every sink: queries, shell, file paths, templates, outbound requests, responses.
3. For each new or changed entry point, confirm authentication and authorization happen before any data access. Compare with how sibling handlers do it.
4. Check what gets logged or returned on errors.

## Not a finding

Theoretical issues with no reachable input path. Hardening ideas with no concrete attack (low at most, and only if cheap to fix).
