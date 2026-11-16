# Severity rubric

- **critical**: exploitable security hole, data loss or corruption, or an outage on a normal path. Must not merge.
- **high**: wrong behavior users will hit, a security weakness that needs specific conditions, or a breaking change for consumers. Should block merge.
- **medium**: a bug in an edge case, a meaningful performance regression, a missing test for risky behavior, or a design problem that will cost real effort later. Fix soon; may not block.
- **low**: minor issue or worthwhile improvement. Author's call.

Judge impact and likelihood in this codebase, not in the abstract. If the path is hard to reach in practice (internal-only caller, guarded upstream), lower the severity at least one level.
