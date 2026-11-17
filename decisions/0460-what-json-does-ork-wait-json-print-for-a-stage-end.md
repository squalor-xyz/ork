---
{
  "id": 460,
  "title": "What JSON does ork wait --json print for a stage end?",
  "status": "decided",
  "key": "cli.wait-stage-json",
  "asked_by": "Jon",
  "decided_by": "Jon",
  "created": "2026-11-17",
  "decided_at": "2026-11-17",
  "context": "S403 spec check, 2026-11-17",
  "options": [
    {
      "label": "waitJSON-stage",
      "summary": "The wait object with state \"stage\" and reason \"ROLE round N ended EXIT\""
    },
    {
      "label": "phrase",
      "summary": "state is the full phrase, reason empty"
    },
    {
      "label": "as-written",
      "summary": "No stage JSON shape; exit 6 only"
    }
  ],
  "recommendation": {
    "label": "waitJSON-stage",
    "reasoning": "a stage end reuses the waitJSON object the slice already requires for several runs"
  },
  "decision": "waitJSON-stage",
  "rationale": "Owner chose 'state \"stage\" + reason' in an interactive question, relay session 8d711f67, 2026-11-17.",
  "refs": [
    "S403"
  ],
  "history": [
    {
      "date": "2026-11-17",
      "event": "ask"
    },
    {
      "date": "2026-11-17",
      "event": "decide"
    }
  ]
}
---
