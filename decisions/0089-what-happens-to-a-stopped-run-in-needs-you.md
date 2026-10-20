---
{
  "id": 89,
  "title": "What happens to a stopped run in needs you?",
  "status": "decided",
  "key": "ui.stopped-runs",
  "asked_by": "claude",
  "decided_by": "jon",
  "created": "2026-10-19",
  "decided_at": "2026-10-19",
  "context": "ork status counts stopped runs under needs you and nothing clears them.",
  "options": [
    {
      "label": "newer-run",
      "summary": "hide once the slice has a newer run"
    },
    {
      "label": "dismiss",
      "summary": "keep until dismissed"
    },
    {
      "label": "age-out",
      "summary": "drop after a set time"
    }
  ],
  "decision": "newer-run",
  "rationale": "Owner 2026-10-20: a stopped run stops needing you once the slice has a newer run.",
  "refs": [
    "S22"
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
