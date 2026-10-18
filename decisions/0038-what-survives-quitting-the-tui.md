---
{
  "id": 38,
  "title": "What survives quitting the TUI?",
  "status": "decided",
  "key": "daemon.lifetime",
  "asked_by": "claude",
  "decided_by": "jon",
  "created": "2026-10-18",
  "decided_at": "2026-10-18",
  "context": "Runs take 20-40 minutes or more.",
  "options": [
    {
      "label": "daemon",
      "summary": "Background daemon plus TUI client"
    },
    {
      "label": "detached",
      "summary": "Detached roles, TUI reconciles"
    },
    {
      "label": "die",
      "summary": "Runs die with the TUI"
    }
  ],
  "decision": "daemon",
  "rationale": "Owner interview 2026-10-18: background daemon owns waves and processes; the TUI is a client over a local socket.",
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
