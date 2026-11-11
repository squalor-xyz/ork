---
{
  "id": 305,
  "title": "slicer snapshot lacks fields the dashboard reads; how does S335 use it?",
  "status": "decided",
  "key": "poll.snapshot-gap",
  "asked_by": "claude",
  "decided_by": "Jon",
  "via": "interactive question, relay session 8d711f67, 2026-11-11",
  "created": "2026-11-11",
  "decided_at": "2026-11-11",
  "options": [
    {
      "label": "revision-probe",
      "summary": "Snapshot reads revisions only; a changed part is re-read per part"
    },
    {
      "label": "goals-only",
      "summary": "Decode only goals from the snapshot"
    },
    {
      "label": "wait-for-slicer",
      "summary": "Hold S335 until slicer's snapshot carries the missing fields"
    }
  ],
  "recommendation": {
    "label": "revision-probe",
    "reasoning": "keeps every field with no slicer change"
  },
  "decision": "wait-for-slicer",
  "rationale": "Owner picked wait for slicer",
  "refs": [
    "S335"
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
