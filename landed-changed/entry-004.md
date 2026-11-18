S409 (*proposed*): Check lines are classified by quotes, not by every
character. A `;`, `|`, `&`, `<` or `>` inside single or double quotes, as in
`go test -run 'A|B'`, no longer makes a Check command orchestrator-only; an
unquoted one still does. `rg` Check lines stay orchestrator-only: the
implementer presets do not allow `rg` (`launch.allowlist-rg`).
