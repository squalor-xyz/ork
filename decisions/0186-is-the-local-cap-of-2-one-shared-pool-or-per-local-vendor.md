---
{
  "id": 186,
  "title": "Is the local cap of 2 one shared pool or per local vendor?",
  "status": "decided",
  "key": "roles.local-model.cap-shape",
  "asked_by": "claude",
  "decided_by": "Jon",
  "recorded_by": "claude",
  "via": "interactive question, ork session 2026-11-06",
  "created": "2026-11-06",
  "decided_at": "2026-11-06",
  "context": "S301.",
  "options": [
    {
      "label": "shared",
      "summary": "one pool of 2 across claude-local, codex-local and grok-local"
    },
    {
      "label": "per-vendor",
      "summary": "2 for each local vendor"
    }
  ],
  "recommendation": {
    "label": "shared",
    "reasoning": "One server serves all three."
  },
  "decision": "shared",
  "rationale": "Owner answered a shared pool of 4 total max, interactive question, 2026-11-06. This conflicts with decided record 182 (cap 2); the owner chose 4 shared, so 182 needs the owner to supersede it.",
  "refs": [
    "S301"
  ],
  "history": [
    {
      "date": "2026-11-06",
      "event": "ask"
    },
    {
      "date": "2026-11-06",
      "event": "decide"
    }
  ]
}
---
