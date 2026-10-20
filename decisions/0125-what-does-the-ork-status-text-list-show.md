---
{
  "id": 125,
  "title": "What does the ork status text list show?",
  "status": "decided",
  "key": "ui.status-list",
  "asked_by": "claude",
  "decided_by": "jon",
  "created": "2026-10-20",
  "decided_at": "2026-10-20",
  "context": "ork status counts needs-you items it does not list (internal/daemon/handlers.go:204,213-215).",
  "options": [
    {
      "label": "hide-superseded-list-adj",
      "summary": "Hide superseded and committed runs as the Overview does (record 92), keep the full list in --json, and print open adj records and needs-owner items as one line each so the count equals the list"
    }
  ],
  "recommendation": {
    "label": "hide-superseded-list-adj",
    "reasoning": "Owner accepted the recommendation 2026-10-20."
  },
  "decision": "hide-superseded-list-adj",
  "rationale": "Owner 2026-10-20: one builder for status and the TUI.",
  "refs": [
    "S32,S33"
  ],
  "history": [
    {
      "date": "2026-10-20",
      "event": "ask"
    },
    {
      "date": "2026-10-20",
      "event": "decide"
    }
  ]
}
---
