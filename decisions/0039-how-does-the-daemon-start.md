---
{
  "id": 39,
  "title": "How does the daemon start?",
  "status": "decided",
  "key": "daemon.start",
  "asked_by": "claude",
  "decided_by": "jon",
  "created": "2026-10-18",
  "decided_at": "2026-10-18",
  "context": "Global rules discourage system services.",
  "options": [
    {
      "label": "on-demand",
      "summary": "On demand"
    },
    {
      "label": "launchd",
      "summary": "launchd agent"
    }
  ],
  "decision": "on-demand",
  "rationale": "Owner interview 2026-10-18: started on demand by ork, Unix socket in ~/.local/state/ork, exits when idle.",
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
