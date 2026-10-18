---
{
  "id": 20,
  "title": "How is machine load capped?",
  "status": "decided",
  "key": "wave.limits",
  "asked_by": "claude",
  "decided_by": "jon",
  "created": "2026-10-18",
  "decided_at": "2026-10-18",
  "context": "About 4 relays on 8 cores was the practical limit.",
  "options": [
    {
      "label": "global-and-project",
      "summary": "Global cap plus per-project cap"
    },
    {
      "label": "per-project",
      "summary": "Per-project cap only"
    },
    {
      "label": "warn",
      "summary": "No cap, warn only"
    }
  ],
  "decision": "global-and-project",
  "rationale": "Owner interview 2026-10-18: global cap (default 4) plus optional per-project cap; extra slices queue.",
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
