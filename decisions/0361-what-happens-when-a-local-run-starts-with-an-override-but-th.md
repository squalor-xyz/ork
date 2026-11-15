---
{
  "id": 361,
  "title": "What happens when a local run starts with an override but the item's cached spec check left no orientation.md?",
  "status": "decided",
  "key": "speccheck.cached-verdict-no-pack",
  "asked_by": "Jon",
  "decided_by": "Jon",
  "created": "2026-11-15",
  "decided_at": "2026-11-15",
  "context": "S366 spec check question.",
  "options": [
    {
      "label": "stale",
      "summary": "the spec check treats a local-planned item with no orientation.md as stale and re-runs; a local run still without a pack starts without one"
    },
    {
      "label": "start-without",
      "summary": "keep the cached verdict; start without a pack"
    },
    {
      "label": "rerun-in-start",
      "summary": "startRun re-runs the spec check"
    }
  ],
  "recommendation": {
    "label": "stale",
    "reasoning": "the CLI runs spec.check before every start, so no long wait inside startRun"
  },
  "decision": "stale",
  "rationale": "Owner chose the recommended option in an interactive question, relay session 8d711f67, 2026-11-15 ('Treat as stale (Recommended)').",
  "refs": [
    "S366"
  ],
  "history": [
    {
      "date": "2026-11-15",
      "event": "ask"
    },
    {
      "date": "2026-11-15",
      "event": "decide"
    }
  ]
}
---
