---
{
  "id": 64,
  "title": "How does the daemon serve dashboard data?",
  "status": "decided",
  "key": "dash.cache",
  "asked_by": "claude",
  "decided_by": "jon",
  "created": "2026-10-19",
  "decided_at": "2026-10-19",
  "context": "Measured about 7 s CPU per ork status across four projects with no cache; slicer calls cost 0.4-1.4 s under load.",
  "options": [
    {
      "label": "daemon-push",
      "summary": "shared in-memory cache, pushed change notices"
    },
    {
      "label": "daemon-ttl",
      "summary": "shared cache, clients poll"
    },
    {
      "label": "none",
      "summary": "call the CLIs per request"
    }
  ],
  "decision": "daemon-push",
  "rationale": "Owner 2026-10-19 (integration review, docs/review-2026-10-19-integration.md): In-memory snapshot in the daemon shared by all clients, deduplicated calls, at most 4 CLI processes at once, last good value kept on error, change notices pushed over the socket. No cache files, no file watching.",
  "history": [
    {
      "date": "2026-10-19",
      "event": "ask"
    },
    {
      "date": "2026-10-19",
      "event": "decide"
    }
  ]
}
---
