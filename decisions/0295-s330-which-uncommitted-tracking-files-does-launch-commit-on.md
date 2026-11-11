---
{
  "id": 295,
  "title": "S330: which uncommitted tracking files does launch commit on main?",
  "status": "decided",
  "key": "launch.tracking-scope",
  "asked_by": "claude",
  "decided_by": "Jon",
  "via": "interactive question, relay session 8d711f67, 2026-11-11",
  "created": "2026-11-11",
  "decided_at": "2026-11-11",
  "options": [
    {
      "label": "item-only",
      "summary": "only .slicer/slices/ID.json, its render and .slicer/index.json"
    },
    {
      "label": "all-tracking",
      "summary": "every tracking path splitTracking returns"
    }
  ],
  "recommendation": {
    "label": "item-only",
    "reasoning": "adj 290 says the item's own tracking files."
  },
  "decision": "item-only",
  "rationale": "Owner picked Item's own only (recommended).",
  "refs": [
    "S330"
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
