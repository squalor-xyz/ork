---
{
  "id": 214,
  "title": "S255: Which retries are \"fresh-round\"?",
  "status": "decided",
  "key": "vendor.retry-chain-reset",
  "decided_by": "Jon",
  "via": "interactive question, relay session 8d711f67, 2026-11-09",
  "created": "2026-11-09",
  "decided_at": "2026-11-09",
  "context": "Spec question drafted 2026-11-09 for S255; owner accepted the recommended answers in an interactive question (relay session 8d711f67).",
  "options": [
    {
      "label": "a",
      "summary": "a rounds-pause retry and a stopped-run resume (recommended: both start a new round, the other pauses rerun the same round, which the owner said stays sticky)"
    },
    {
      "label": "b",
      "summary": "every `ork retry`"
    },
    {
      "label": "c",
      "summary": "only a rounds-pause retry."
    }
  ],
  "recommendation": {
    "label": "a",
    "reasoning": "Drafter recommendation."
  },
  "decision": "a",
  "rationale": "Owner accepted the recommended answers for the drafted specs of S255, S257, S259, S263, S267, S283 and S293 on 2026-11-09.",
  "refs": [
    "S255"
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
