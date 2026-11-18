*Proposed (S317):* run the slice's plain backticked Check commands before
the gate and at the landing re-check, with environment prefixes, per-row
deadlines, exit status and logs. The gate receives every row as tests-green
evidence. Failed rows send their logs to a fix round; docs/tracking-only
fixes reuse the earlier result. Unsupported shell commands stay with the
orchestrator. Human and after-land checks keep their existing workflow.
