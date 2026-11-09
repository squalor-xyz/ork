---
{
  "id": 249,
  "title": "S262: how should the two orchestrator-only Check commands be run?",
  "status": "decided",
  "key": "process.s262-check-lines",
  "decided_by": "Jon",
  "via": "interactive question, relay session 8d711f67, 2026-11-09",
  "created": "2026-11-09",
  "decided_at": "2026-11-09",
  "context": "Raised by the S262 spec check; owner answered interactively, relay session 8d711f67.",
  "options": [
    {
      "label": "1",
      "summary": "replace with runnable forms: go test -race -count=3 ./internal/tui -run Running|Wave|Launch|Tab and an rg for the dead code"
    },
    {
      "label": "2",
      "summary": "assign both lines to the orchestrator"
    },
    {
      "label": "3",
      "summary": "keep as written"
    }
  ],
  "recommendation": {
    "label": "1",
    "reasoning": "Spec-check recommendation."
  },
  "decision": "1",
  "rationale": "Owner picked option 1 on 2026-11-09.",
  "refs": [
    "S262"
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
