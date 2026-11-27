---
{
  "id": 660,
  "title": "What does retry on file exists mean for chain and wave IDs?",
  "status": "decided",
  "key": "run.id-collision-retry",
  "asked_by": "Jon",
  "decided_by": "Jon",
  "created": "2026-11-27",
  "decided_at": "2026-11-27",
  "context": "S654 spec draft.",
  "options": [
    {
      "label": "widen-only",
      "summary": "chain and wave IDs get 4 random bytes; only the run ID create retries"
    },
    {
      "label": "check-store",
      "summary": "scan stored runs and retry"
    }
  ],
  "recommendation": {
    "label": "widen-only",
    "reasoning": "Recommended in the interactive question."
  },
  "decision": "widen-only",
  "rationale": "Owner chose 'Widen only' in an interactive question, relay session 8d711f67, 2026-11-27.",
  "refs": [
    "S654"
  ],
  "history": [
    {
      "date": "2026-11-27",
      "event": "ask"
    },
    {
      "date": "2026-11-27",
      "event": "decide"
    }
  ]
}
---
