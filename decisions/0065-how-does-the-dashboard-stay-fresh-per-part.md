---
{
  "id": 65,
  "title": "How does the dashboard stay fresh (per part)?",
  "status": "decided",
  "key": "dash.refresh",
  "asked_by": "claude",
  "decided_by": "jon",
  "created": "2026-10-19",
  "decided_at": "2026-10-19",
  "context": "Supersedes the uniform schedule; goals and decided records change rarely.",
  "options": [
    {
      "label": "per-part",
      "summary": "per-part schedule with git HEAD trigger"
    },
    {
      "label": "uniform",
      "summary": "every part on 5 s / 30 s"
    }
  ],
  "decision": "per-part",
  "rationale": "Owner 2026-10-19 (integration review, docs/review-2026-10-19-integration.md): Items and runs: visible project every 5 s, others every 30 s. Goals and decided records: every 5 min, on a git HEAD change, on tab open and on r.",
  "supersedes": [
    52
  ],
  "history": [
    {
      "date": "2026-10-19",
      "event": "ask"
    },
    {
      "date": "2026-10-19",
      "event": "decide"
    },
    {
      "date": "2026-10-19",
      "event": "supersede",
      "note": "supersedes 52"
    }
  ]
}
---
