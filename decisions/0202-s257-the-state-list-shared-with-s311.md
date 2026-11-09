---
{
  "id": 202,
  "title": "S257: The state list shared with S311",
  "status": "decided",
  "key": "cli.run-event-states",
  "decided_by": "Jon",
  "via": "interactive question, relay session 8d711f67, 2026-11-09",
  "created": "2026-11-09",
  "decided_at": "2026-11-09",
  "context": "Spec question drafted 2026-11-09 for S257; owner accepted the recommended answers in an interactive question (relay session 8d711f67).",
  "options": [
    {
      "label": "a",
      "summary": "`question`, `awaiting-approval`, `paused`, `stopped`, `landed`, `failed`, where `failed` means a ledger run that ended with outcome fail (needs a read of the ledger, unverified) (recommended: the list S311 already names)"
    },
    {
      "label": "b",
      "summary": "drop `failed` until a record state exists"
    },
    {
      "label": "c",
      "summary": "use raw record states only."
    }
  ],
  "recommendation": {
    "label": "a",
    "reasoning": "Drafter recommendation."
  },
  "decision": "a",
  "rationale": "Owner accepted the recommended answers for the drafted specs of S255, S257, S259, S263, S267, S283 and S293 on 2026-11-09.",
  "refs": [
    "S257"
  ],
  "history": [
    {
      "date": "2026-11-09",
      "event": "ask"
    },
    {
      "date": "2026-11-09",
      "event": "decide"
    }
  ]
}
---
