---
{
  "id": 256,
  "title": "S285: how should the orchestrator-only Check line be handled?",
  "status": "decided",
  "key": "process.s285-check-line",
  "decided_by": "Jon",
  "via": "interactive question, relay session 8d711f67, 2026-11-09",
  "created": "2026-11-09",
  "decided_at": "2026-11-09",
  "context": "Raised by the S285 spec check; owner answered interactively, relay session 8d711f67.",
  "options": [
    {
      "label": "1",
      "summary": "replace with the runnable form go test -race -count=1 ./internal/engine ./internal/daemon ./internal/cli -run RoundCheck|FullCheck|LandRecheck|ProjectsSet|CheckCommand"
    },
    {
      "label": "2",
      "summary": "orchestrator runs it as written"
    },
    {
      "label": "3",
      "summary": "drop the line"
    }
  ],
  "recommendation": {
    "label": "1",
    "reasoning": "Spec-check recommendation."
  },
  "decision": "1",
  "rationale": "Owner picked option 1 on 2026-11-09.",
  "refs": [
    "S285"
  ],
  "history": [
    {
      "date": "2026-11-09",
      "event": "ask"
    },
    {
      "date": "2026-11-09",
      "event": "decide"
    }
  ]
}
---
