---
{
  "id": 279,
  "title": "May a per-project setting that runs never read (S224 bell, S311 notify) change while the project has unfinished runs?",
  "status": "decided",
  "key": "projects.set-busy-unread-fields",
  "asked_by": "claude",
  "decided_by": "Jon",
  "via": "interactive question, relay session 8d711f67, 2026-11-11",
  "created": "2026-11-11",
  "decided_at": "2026-11-11",
  "context": "S224 spec check 2026-11-11: since S265 the busy rule refuses only fields a started run reads (internal/daemon/handlers.go:571-575); bell is read only by the TUI at start.",
  "options": [
    {
      "label": "refuse",
      "summary": "bell joins fieldsReadByARun; an unfinished run gives project_busy naming --bell"
    },
    {
      "label": "allow",
      "summary": "bell stays out of fieldsReadByARun; the change succeeds while runs are unfinished, like --cap and the budgets"
    }
  ],
  "recommendation": {
    "label": "allow",
    "reasoning": "Runs never read it; the busy rule covers only fields a started run reads."
  },
  "decision": "allow",
  "rationale": "Owner picked Allow (recommended) in an interactive question on 2026-11-11; applies to S224 and S311.",
  "refs": [
    "S224",
    "S311"
  ],
  "history": [
    {
      "date": "2026-11-11",
      "event": "ask"
    },
    {
      "date": "2026-11-11",
      "event": "decide"
    }
  ]
}
---
