---
{
  "id": 287,
  "title": "S333: what does expanding a Running run with no working role show?",
  "status": "decided",
  "key": "ui.running-expand-idle",
  "asked_by": "claude",
  "decided_by": "Jon",
  "via": "interactive question, relay session 8d711f67, 2026-11-11",
  "created": "2026-11-11",
  "decided_at": "2026-11-11",
  "context": "S333 reviewer round 1, 2026-11-11: adj 283 covers rows with active roles; paused, question and stopped runs have none.",
  "options": [
    {
      "label": "facts",
      "summary": "one dim line of the strip's facts (state, what it waits on or failed with) under the open row"
    },
    {
      "label": "no-expand",
      "summary": "such rows are not expandable; E skips them"
    },
    {
      "label": "as-is",
      "summary": "expanding adds no line"
    }
  ],
  "recommendation": {
    "label": "facts",
    "reasoning": "Facts the row lacks (adj 283); E then shows every waiting question."
  },
  "decision": "facts",
  "rationale": "Owner picked One facts line (recommended).",
  "refs": [
    "S333"
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
