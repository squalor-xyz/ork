---
{
  "id": 306,
  "title": "Where does the cache's batched read live?",
  "status": "decided",
  "key": "poll.batch-shape",
  "asked_by": "claude",
  "decided_by": "Jon",
  "via": "interactive question, relay session 8d711f67, 2026-11-11",
  "created": "2026-11-11",
  "decided_at": "2026-11-11",
  "options": [
    {
      "label": "optional-interface",
      "summary": "Direct implements an optional batch interface found by type assertion; due reads coalesce in a short window"
    },
    {
      "label": "cache-tick",
      "summary": "One cache-level timer for the three parts"
    },
    {
      "label": "extend-fetcher",
      "summary": "Add a batch method to source.Fetcher"
    }
  ],
  "recommendation": {
    "label": "optional-interface",
    "reasoning": "smallest change; per-part fallback stays on the existing path"
  },
  "decision": "optional-interface",
  "rationale": "Owner picked optional interface",
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
