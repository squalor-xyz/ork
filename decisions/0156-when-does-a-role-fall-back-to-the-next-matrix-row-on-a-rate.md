---
{
  "id": 156,
  "title": "When does a role fall back to the next matrix row: on a rate limit or on an error too?",
  "status": "decided",
  "key": "vendor.fallback-trigger-v2",
  "decided_by": "Jon R",
  "created": "2026-10-23",
  "decided_at": "2026-10-23",
  "context": "Owner 2026-10-23: 'rate limit or error is right, and we should unify on this', replacing record 114 (rate limit only). Record 23 says anything unclassified stops the slice for the owner. This record defines the unified trigger so 114 can be superseded by it. Unify means S49's text, design.md:303,418 and adj 100 (which says 'fallback to the Claude row on rate limit or error') all say the same.",
  "options": [
    {
      "label": "rate-limit-or-error",
      "summary": "Fall back to the next row when the exit is classified rate-limited or error; a denied exit and an unclassified exit still stop the slice for the owner; resume only a session from the same vendor and model, otherwise start fresh",
      "tradeoffs": "Codex errors (a bad model, a crash) move to the Claude row without stopping; keeps record 23 for unclassified exits; denials still need the owner."
    },
    {
      "label": "any-nonok",
      "summary": "Fall back on every non-ok exit except denied, including unclassified",
      "tradeoffs": "Never stops for an unknown exit while a next row exists; changes record 23 and could hide a real failure behind a fallback."
    }
  ],
  "recommendation": {
    "label": "rate-limit-or-error",
    "reasoning": "Matches the owner's words and S49's text, keeps record 23 for exits ork cannot classify, and a denial is the owner's call."
  },
  "decision": "rate-limit-or-error",
  "rationale": "Owner 2026-10-23: rate limit or error is right; unify on it. Keeps record 23 for unclassified exits.",
  "supersedes": [
    114
  ],
  "refs": [
    "S49",
    "S47"
  ],
  "history": [
    {
      "date": "2026-10-23",
      "event": "ask"
    },
    {
      "date": "2026-10-23",
      "event": "decide"
    },
    {
      "date": "2026-10-23",
      "event": "supersede",
      "note": "supersedes 114"
    }
  ]
}
---
