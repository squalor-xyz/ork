---
{
  "id": 92,
  "title": "What counts as a newer run for a stopped engine run?",
  "status": "decided",
  "key": "ui.stopped-newer",
  "asked_by": "claude",
  "decided_by": "jon",
  "created": "2026-10-20",
  "decided_at": "2026-10-20",
  "context": "ui.stopped-runs hides a stopped run once the slice has a newer run; the wire has no ordering field.",
  "options": [
    {
      "label": "engine-newer",
      "summary": "a newer engine run for the same slice, ordered by the record's created time"
    },
    {
      "label": "any-newer",
      "summary": "engine or ledger run"
    }
  ],
  "decision": "engine-newer",
  "rationale": "Owner 2026-10-20: a newer engine run for the same slice, ordered by created time. Ledger runs are ignored. It also changes the ork status needs-you count.",
  "refs": [
    "S22"
  ],
  "history": [
    {
      "date": "2026-10-20",
      "event": "ask"
    },
    {
      "date": "2026-10-20",
      "event": "decide"
    }
  ]
}
---
