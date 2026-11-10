---
{
  "id": 259,
  "title": "S295: how should the orchestrator-only first Check command be made runnable?",
  "status": "decided",
  "key": "process.s295-check-line",
  "decided_by": "Jon",
  "via": "interactive question, relay session 8d711f67, 2026-11-10",
  "created": "2026-11-10",
  "decided_at": "2026-11-10",
  "context": "Raised by the S295 spec check; owner answered interactively, relay session 8d711f67.",
  "options": [
    {
      "label": "1",
      "summary": "tighten: go test -count=1 ./internal/engine ./internal/daemon ./internal/cli with no -run filter; mutation step stays manual"
    },
    {
      "label": "2",
      "summary": "orchestrator runs the filtered -run line"
    },
    {
      "label": "3",
      "summary": "as written"
    }
  ],
  "recommendation": {
    "label": "1",
    "reasoning": "Spec-check recommendation."
  },
  "decision": "1",
  "rationale": "Owner picked option 1 on 2026-11-10.",
  "refs": [
    "S295"
  ],
  "history": [
    {
      "date": "2026-11-10",
      "event": "ask"
    },
    {
      "date": "2026-11-10",
      "event": "decide"
    }
  ]
}
---
