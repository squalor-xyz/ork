---
{
  "id": 67,
  "title": "How does ork check slicer's relay contract version?",
  "status": "decided",
  "key": "boundary.contract",
  "asked_by": "claude",
  "decided_by": "jon",
  "created": "2026-10-19",
  "decided_at": "2026-10-19",
  "context": "Supersedes the exact pin: slicer s382 bumps the contract to v8, which an exact v7 pin would refuse.",
  "options": [
    {
      "label": "accepted-set",
      "summary": "a list of supported versions"
    },
    {
      "label": "minimum",
      "summary": "v7 or newer"
    },
    {
      "label": "exact-pin",
      "summary": "exact pin, bumped by hand"
    }
  ],
  "decision": "accepted-set",
  "rationale": "Owner 2026-10-19 (integration review, docs/review-2026-10-19-integration.md): ork lists the contract versions it supports and refuses any other; s378 should state what each bump changed. Before S04.",
  "supersedes": [
    6
  ],
  "history": [
    {
      "date": "2026-10-19",
      "event": "ask"
    },
    {
      "date": "2026-10-19",
      "event": "decide"
    },
    {
      "date": "2026-10-19",
      "event": "supersede",
      "note": "supersedes 6"
    }
  ]
}
---
