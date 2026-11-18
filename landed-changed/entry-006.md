S380 (*proposed*): A step records its own cost and tokens, not the session
total. A resumed Claude session reports its running cost and a resumed Codex
thread its running tokens; ork now stores the step's part (new role fields
`session_tokens` and `session_cost_usd` keep the vendor's total), so run
totals and token budgets stop counting the same usage twice. Older run
records are read as they are.
