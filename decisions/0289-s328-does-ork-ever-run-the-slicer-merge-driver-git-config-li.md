---
{
  "id": 289,
  "title": "S328: does ork ever run the slicer merge-driver git config lines itself?",
  "status": "decided",
  "key": "doctor.merge-driver-fix",
  "asked_by": "claude",
  "decided_by": "Jon",
  "via": "interactive question, relay session 8d711f67, 2026-11-11",
  "created": "2026-11-11",
  "decided_at": "2026-11-11",
  "options": [
    {
      "label": "print-only",
      "summary": "ork prints the lines; the owner runs them"
    },
    {
      "label": "doctor-fix",
      "summary": "ork doctor --fix runs them after a [y/N] prompt"
    }
  ],
  "recommendation": {
    "label": "print-only",
    "reasoning": "It is the clone's git config, outside ork's files."
  },
  "decision": "doctor-fix",
  "rationale": "Owner picked doctor --fix (not the recommendation).",
  "refs": [
    "S328"
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
