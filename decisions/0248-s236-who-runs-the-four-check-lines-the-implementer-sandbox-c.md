---
{
  "id": 248,
  "title": "S236: who runs the four Check lines the implementer sandbox cannot run?",
  "status": "decided",
  "key": "process.s236-check-lines",
  "decided_by": "Jon",
  "via": "interactive question, relay session 8d711f67, 2026-11-09",
  "created": "2026-11-09",
  "decided_at": "2026-11-09",
  "context": "Raised by the S236 spec check; owner answered interactively, relay session 8d711f67.",
  "options": [
    {
      "label": "1",
      "summary": "assign to orchestrator: it runs the -race -count=3 line, both GOOS vet lines and the ps line; the implementer runs the rest"
    },
    {
      "label": "2",
      "summary": "replace with runnable forms; cross-OS vet and ps become dropped or a HUMAN CHECK"
    }
  ],
  "recommendation": {
    "label": "1",
    "reasoning": "Spec-check recommendation."
  },
  "decision": "1",
  "rationale": "Owner picked option 1 on 2026-11-09.",
  "refs": [
    "S236"
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
