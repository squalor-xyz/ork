---
{
  "id": 13,
  "title": "What format do ork's state files use?",
  "status": "decided",
  "key": "state.format",
  "asked_by": "claude",
  "decided_by": "jon",
  "created": "2026-10-18",
  "decided_at": "2026-10-18",
  "context": "docs/state.md proposes JSON, as slicer and adj use.",
  "options": [
    {
      "label": "json",
      "summary": "JSON, versioned, atomic writes"
    },
    {
      "label": "toml",
      "summary": "TOML for the hand-edited registry, JSON for run records"
    }
  ],
  "recommendation": {
    "label": "json",
    "reasoning": "stdlib encoding/json; matches sibling tools."
  },
  "decision": "json",
  "rationale": "Owner 2026-10-18: JSON, versioned, atomic writes; stdlib encoding/json, as slicer and adj.",
  "history": [
    {
      "date": "2026-10-18",
      "event": "ask"
    },
    {
      "date": "2026-10-18",
      "event": "decide"
    }
  ]
}
---
