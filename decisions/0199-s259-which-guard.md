---
{
  "id": 199,
  "title": "S259: Which guard?",
  "status": "decided",
  "key": "watchdog.wake-guard",
  "decided_by": "Jon",
  "via": "interactive question, relay session 8d711f67, 2026-11-09",
  "created": "2026-11-09",
  "decided_at": "2026-11-09",
  "context": "Spec question drafted 2026-11-09 for S259; owner accepted the recommended answers in an interactive question (relay session 8d711f67).",
  "options": [
    {
      "label": "a",
      "summary": "count sleep only after `Start` (recommended: the finding's second form; a never-started Wake can only be wrong, so it counts nothing and the watchdog is exact)"
    },
    {
      "label": "b",
      "summary": "raise `wakeGap` to 2 minutes, above the 30 s tick (the finding's first form; it still misjudges if the tick is ever longer)"
    },
    {
      "label": "c",
      "summary": "both."
    }
  ],
  "recommendation": {
    "label": "a",
    "reasoning": "Drafter recommendation."
  },
  "decision": "a",
  "rationale": "Owner accepted the recommended answers for the drafted specs of S255, S257, S259, S263, S267, S283 and S293 on 2026-11-09.",
  "refs": [
    "S259"
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
