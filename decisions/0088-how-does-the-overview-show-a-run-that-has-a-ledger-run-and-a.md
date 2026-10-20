---
{
  "id": 88,
  "title": "How does the Overview show a run that has a ledger run and an engine run?",
  "status": "decided",
  "key": "ui.run-join",
  "asked_by": "claude",
  "decided_by": "jon",
  "created": "2026-10-19",
  "decided_at": "2026-10-19",
  "context": "internal/tui/overview.go:95 shows only ledger runs, labelled started elsewhere; ork status also reports engine runs (S17).",
  "options": [
    {
      "label": "join",
      "summary": "one row, joined by slice and worktree; engine state and wait lead, ledger steps and time fill in; a ledger run with no engine run stays started elsewhere"
    },
    {
      "label": "engine-only",
      "summary": "hide the ledger run when an engine run exists"
    },
    {
      "label": "two-rows",
      "summary": "show both"
    }
  ],
  "decision": "join",
  "rationale": "Owner 2026-10-20: one row, joined by slice and worktree.",
  "refs": [
    "S22"
  ],
  "history": [
    {
      "date": "2026-10-19",
      "event": "ask"
    },
    {
      "date": "2026-10-19",
      "event": "decide"
    }
  ]
}
---
