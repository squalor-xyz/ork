---
{
  "id": 356,
  "title": "What does the notify 'failed' event mean, given no run state or ledger read produces it?",
  "status": "decided",
  "key": "notify.failed-event",
  "asked_by": "Jon",
  "decided_by": "Jon",
  "created": "2026-11-15",
  "decided_at": "2026-11-15",
  "context": "S311 spec check question.",
  "options": [
    {
      "label": "defer",
      "summary": "notify on question, awaiting-approval, paused, stopped and landed only; docs say failed waits for a ledger read; narrows adj 221 as adj 247 narrowed adj 202"
    },
    {
      "label": "add-ledger-read",
      "summary": "also read slicer ledger runs that ended with outcome fail and notify ORK_EVENT=failed"
    },
    {
      "label": "as-written",
      "summary": "keep adj 221 and leave failed undefined"
    }
  ],
  "recommendation": {
    "label": "defer",
    "reasoning": "the run record does not say a ledger run failed (adj 247); a failing run already ends as stopped, which notifies"
  },
  "decision": "defer",
  "rationale": "Owner chose the recommended option in an interactive question, relay session 8d711f67, 2026-11-15 ('Defer (Recommended)').",
  "refs": [
    "S311"
  ],
  "history": [
    {
      "date": "2026-11-15",
      "event": "ask"
    },
    {
      "date": "2026-11-15",
      "event": "decide"
    }
  ]
}
---
