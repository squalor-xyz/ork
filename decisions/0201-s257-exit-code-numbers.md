---
{
  "id": 201,
  "title": "S257: Exit code numbers",
  "status": "decided",
  "key": "cli.wait-exit-codes",
  "decided_by": "Jon",
  "via": "interactive question, relay session 8d711f67, 2026-11-09",
  "created": "2026-11-09",
  "decided_at": "2026-11-09",
  "context": "Spec question drafted 2026-11-09 for S257; owner accepted the recommended answers in an interactive question (relay session 8d711f67).",
  "options": [
    {
      "label": "a",
      "summary": "0 changed to an until state, 3 question pending, 4 nothing active, 5 timeout (recommended: keeps 1 failure and 2 usage, and codes sit above 2)"
    },
    {
      "label": "b",
      "summary": "0 for state changed, question pending, nothing active and timeout each separate, with timeout 124 as `timeout(1)` does"
    },
    {
      "label": "c",
      "summary": "10-13."
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
