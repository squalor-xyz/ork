---
{
  "id": 395,
  "title": "What does ork do when a slice above size S cannot be slivered under contract rule u6, or the carver cannot cut it to effort-1 slivers?",
  "status": "decided",
  "key": "roles.local-sliver-ineligible",
  "asked_by": "Jon",
  "decided_by": "Jon",
  "created": "2026-11-16",
  "decided_at": "2026-11-16",
  "context": "S359 spec check question.",
  "options": [
    {
      "label": "refuse-local",
      "summary": "ork refuses a non-slivered size M or L slice, or an effort 3 slice, on a local implementer and tells the owner to pick a cloud implementer"
    },
    {
      "label": "run-whole",
      "summary": "run it whole on the local implementer with a warning"
    },
    {
      "label": "carve-anyway",
      "summary": "sliver regardless of u6"
    }
  ],
  "recommendation": {
    "label": "refuse-local",
    "reasoning": "u6 forbids slivering it; S304 trial 5 showed a local implementer cannot finish a whole slice above size S"
  },
  "decision": "refuse-local",
  "rationale": "Owner chose 'Refuse local (Recommended)' in an interactive question, relay session 8d711f67, 2026-11-16.",
  "refs": [
    "S359"
  ],
  "history": [
    {
      "date": "2026-11-16",
      "event": "ask"
    },
    {
      "date": "2026-11-16",
      "event": "decide"
    }
  ]
}
---
