---
{
  "id": 608,
  "title": "Does a note recorded after a cached spec-check verdict make the check run again?",
  "status": "decided",
  "key": "spec.note-after-spec-cache",
  "asked_by": "Jon",
  "decided_by": "Jon",
  "created": "2026-11-25",
  "decided_at": "2026-11-25",
  "context": "S617 spec draft.",
  "options": [
    {
      "label": "fingerprint",
      "summary": "the spec fingerprint also hashes flagged note ids; a new flagged note re-runs the check once"
    },
    {
      "label": "never-cache",
      "summary": "never cache a slice with a flagged note"
    }
  ],
  "recommendation": {
    "label": "fingerprint",
    "reasoning": "Recommended in the interactive question."
  },
  "decision": "fingerprint",
  "rationale": "Owner chose 'Fold into fingerprint' in an interactive question, relay session 8d711f67, 2026-11-25.",
  "refs": [
    "S617"
  ],
  "history": [
    {
      "date": "2026-11-25",
      "event": "ask"
    },
    {
      "date": "2026-11-25",
      "event": "decide"
    }
  ]
}
---
