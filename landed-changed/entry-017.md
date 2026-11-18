S372: a claude-local implementer's Read is capped at 150 lines. A larger
limit, or a Read with no limit of a longer file, is denied with guidance
to Grep for the symbol and Read 150 lines around it. Read-only local roles
stay at 500 lines with the previous denial. The local system prompt and
the implementer's brief name those caps (roles.local-read-cap). Docs
updated: `docs/design.md`.
