---
{
  "id": 446,
  "title": "Does each sliver of a chain need its own landed-changed/ entry?",
  "status": "decided",
  "key": "changelog.sliver-entries",
  "asked_by": "Jon",
  "decided_by": "Jon",
  "created": "2026-11-17",
  "decided_at": "2026-11-17",
  "context": "S332 spec question, 2026-11-17.",
  "options": [
    {
      "label": "each-sliver",
      "summary": "the check uses the run's own item id"
    },
    {
      "label": "parent-only",
      "summary": "only the parent, written by the last sliver"
    }
  ],
  "recommendation": {
    "label": "each-sliver",
    "reasoning": "matches slicer's one entry per item"
  },
  "decision": "each-sliver",
  "rationale": "Owner chose 'Each sliver (Recommended)' in an interactive question, relay session 8d711f67, 2026-11-17.",
  "refs": [
    "S332"
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
