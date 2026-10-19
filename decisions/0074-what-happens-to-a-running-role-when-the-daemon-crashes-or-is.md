---
{
  "id": 74,
  "title": "What happens to a running role when the daemon crashes or is killed?",
  "status": "decided",
  "key": "engine.restart",
  "asked_by": "claude",
  "decided_by": "jon",
  "created": "2026-10-19",
  "decided_at": "2026-10-19",
  "context": "Roles run in their own process groups; the daemon records them in run.json.",
  "options": [
    {
      "label": "mark-lost",
      "summary": "Kill if alive, mark lost, pause for the owner"
    },
    {
      "label": "reattach",
      "summary": "Reattach by pgid and read the report on exit"
    }
  ],
  "decision": "mark-lost",
  "rationale": "Owner 2026-10-19: on start, kill a role still alive but not attached, mark it lost, pause the slice in needs-you; the owner retries the role or stops.",
  "refs": [
    "S04"
  ],
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
