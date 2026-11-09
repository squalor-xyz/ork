---
{
  "id": 222,
  "title": "S311: how the command gets the event",
  "status": "decided",
  "key": "notify.event-delivery",
  "decided_by": "Jon",
  "via": "interactive question, relay session 8d711f67, 2026-11-09",
  "created": "2026-11-09",
  "decided_at": "2026-11-09",
  "context": "Spec question for S311; owner accepted the recommended answers in an interactive question (relay session 8d711f67).",
  "options": [
    {
      "label": "a",
      "summary": "environment variables (`ORK_EVENT`, `ORK_PROJECT`, `ORK_RUN`, `ORK_ITEM`, `ORK_STATE`, `ORK_REASON`, `ORK_NEXT`) and no arguments (recommended: no shell quoting of run text)"
    },
    {
      "label": "b",
      "summary": "a JSON line on standard input"
    },
    {
      "label": "c",
      "summary": "both"
    }
  ],
  "recommendation": {
    "label": "a",
    "reasoning": "Drafter recommendation."
  },
  "decision": "a",
  "rationale": "Owner accepted the recommended answers for the open questions of S311 on 2026-11-09.",
  "refs": [
    "S311"
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
