---
{
  "id": 60,
  "title": "How long does an idle daemon wait before exiting?",
  "status": "decided",
  "key": "daemon.idle",
  "asked_by": "claude",
  "decided_by": "jon",
  "created": "2026-10-18",
  "decided_at": "2026-10-18",
  "context": "design.md proposed 10 minutes; idle means no wave running and no client connected.",
  "options": [
    {
      "label": "10m",
      "summary": "10 minutes, registry key daemon_idle_minutes"
    },
    {
      "label": "never",
      "summary": "never exit on idle"
    },
    {
      "label": "1m",
      "summary": "one minute"
    }
  ],
  "decision": "10m",
  "rationale": "Owner 2026-10-19: 10 minutes, set in the registry as daemon_idle_minutes; 0 means the default (registry.zero-values).",
  "refs": [
    "S03"
  ],
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
