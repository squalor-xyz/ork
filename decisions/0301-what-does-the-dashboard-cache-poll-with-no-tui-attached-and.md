---
{
  "id": 301,
  "title": "What does the dashboard cache poll with no TUI attached, and the live-run interval with one?",
  "status": "decided",
  "key": "poll.idle-backoff",
  "asked_by": "claude",
  "decided_by": "Jon",
  "via": "interactive question, relay session 8d711f67, 2026-11-11",
  "created": "2026-11-11",
  "decided_at": "2026-11-11",
  "options": [
    {
      "label": "idle-none-live-2s",
      "summary": "No timed reads with no TUI; attach refreshes shown projects; live runs every 2 s"
    },
    {
      "label": "idle-none-live-1s",
      "summary": "No timed reads with no TUI; live runs stay 1 s"
    },
    {
      "label": "idle-live-30s",
      "summary": "With no TUI, projects with a live run read every 30 s; live 2 s with a TUI"
    }
  ],
  "recommendation": {
    "label": "idle-none-live-2s",
    "reasoning": "each slicer call costs about 100 ms"
  },
  "decision": "idle-none-live-2s",
  "rationale": "Owner picked nothing idle, live 2 s",
  "refs": [
    "S336"
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
