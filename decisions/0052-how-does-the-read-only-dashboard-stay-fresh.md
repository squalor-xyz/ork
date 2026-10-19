---
{
  "id": 52,
  "title": "How does the read-only dashboard stay fresh?",
  "status": "decided",
  "key": "dash.refresh",
  "asked_by": "claude",
  "decided_by": "jon",
  "created": "2026-10-18",
  "decided_at": "2026-10-18",
  "context": "fsnotify would be a new dependency, and stat-ing .slicer/ is close to slicer's tracking rule. slicer list takes about 0.1 s, adj inbox about 3 ms.",
  "options": [
    {
      "label": "poll",
      "summary": "poll the CLIs, 5 s visible, 30 s others, r to refresh"
    },
    {
      "label": "manual",
      "summary": "load on start, tab switch and r only"
    }
  ],
  "decision": "poll",
  "rationale": "Owner 2026-10-19: Poll the CLIs: the visible project every 5 s, the others every 30 s, r refreshes now; a slow call shows as pending.",
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
