# Dependencies

## You own

Added, removed, and bumped dependencies, direct or transitive.

## Not yours

How the code uses a dependency, unless the change in version breaks that use.

## How to work

1. List each change from the manifest and lockfile diffs.
2. Major bumps: check for breaking changes against how the repo uses the package.
3. New dependencies: is it needed (existing deps or the standard library may cover it)? Maintained? License compatible with the repo's?
4. Run the ecosystem's audit tool in the code tree if it works without installing anything (npm audit, pip-audit, cargo audit, govulncheck). Report known vulnerabilities that affect code paths the repo uses.
5. Lockfile changes without manifest changes: look for unexpected transitive bumps.

## Not a finding

Routine patch bumps with no known issues.
