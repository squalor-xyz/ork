---
{
  "id": 657,
  "title": "What suffix width, retry bound and scope do run, chain and wave IDs use?",
  "status": "decided",
  "key": "run.id-suffix",
  "asked_by": "Jon",
  "decided_by": "Jon",
  "created": "2026-11-27",
  "decided_at": "2026-11-27",
  "context": "S654 spec check.",
  "options": [
    {
      "label": "4b-8tries-all",
      "summary": "4 random bytes (8 hex digits), retry the create up to 8 times on file exists, for run, chain and wave IDs"
    },
    {
      "label": "run-only",
      "summary": "run IDs only"
    }
  ],
  "recommendation": {
    "label": "4b-8tries-all",
    "reasoning": "Recommended in the interactive question."
  },
  "decision": "4b-8tries-all",
  "rationale": "Owner chose '4 bytes, 8 tries, all three' in an interactive question, relay session 8d711f67, 2026-11-27.",
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
