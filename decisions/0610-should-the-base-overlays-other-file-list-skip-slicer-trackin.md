---
{
  "id": 610,
  "title": "Should the Base overlays other-file list skip slicer tracking files (.slicer/...)?",
  "status": "decided",
  "key": "base-overlays.tracking-paths",
  "asked_by": "Jon",
  "decided_by": "Jon",
  "created": "2026-11-25",
  "decided_at": "2026-11-25",
  "context": "Question: Should the Base overlays other-file list skip slicer tracking files (.slicer/...)?\nOptions:\n1. filter-now: skip trackingPath paths from otherFiles in this slice, with one test\n2. follow-up: land as specced and file a slice to filter tracking paths\n3. keep: list tracking files like any other changed file\nRecommend: 2 - the slice matches its spec and the owner decisions; filtering is a small, separate change\nDecision key: base-overlays.tracking-paths",
  "decision": "follow-up: land as specced and file a slice to filter tracking paths (Jon chose 'Follow-up', session 8d711f67, 2026-11-26)",
  "rationale": "Answered in ork on 2026-11-26: Should the Base overlays other-file list skip slicer tracking files (.slicer/...)?",
  "refs": [
    "S618"
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
