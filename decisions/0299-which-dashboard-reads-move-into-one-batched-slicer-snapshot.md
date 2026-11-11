---
{
  "id": 299,
  "title": "Which dashboard reads move into one batched slicer snapshot call?",
  "status": "decided",
  "key": "poll.snapshot-parts",
  "asked_by": "claude",
  "decided_by": "Jon",
  "via": "interactive question, relay session 8d711f67, 2026-11-11",
  "created": "2026-11-11",
  "decided_at": "2026-11-11",
  "options": [
    {
      "label": "items-runs-goals",
      "summary": "Items, runs and goals move; deps, adj parts and git HEAD stay per-project"
    },
    {
      "label": "also-deps",
      "summary": "Also wait for slicer's deps part and move deps"
    }
  ],
  "recommendation": {
    "label": "items-runs-goals",
    "reasoning": "snapshot has no deps part yet (slicer s455)"
  },
  "decision": "items-runs-goals",
  "rationale": "Owner picked items, runs, goals",
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
