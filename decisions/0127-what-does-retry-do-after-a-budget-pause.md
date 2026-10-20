---
{
  "id": 127,
  "title": "What does retry do after a budget pause?",
  "status": "decided",
  "key": "run.budget-resume",
  "asked_by": "claude",
  "decided_by": "jon",
  "created": "2026-10-20",
  "decided_at": "2026-10-20",
  "context": "Budgets are copied into run.json at setup and never enforced (internal/engine/engine.go:186).",
  "options": [
    {
      "label": "one-more-round",
      "summary": "A budget pause waits for the owner; retry grants exactly one more role round, then the check applies again; MaxRounds stays; the minutes clock counts role run time (RoleRecord StartedAt to EndedAt), not wall time"
    }
  ],
  "recommendation": {
    "label": "one-more-round",
    "reasoning": "Owner accepted the recommendation 2026-10-20."
  },
  "decision": "one-more-round",
  "rationale": "Owner 2026-10-20: without a grant, retry would pause again at once; MaxRounds limits red loops, not spend; time spent paused must not count.",
  "refs": [
    "S65"
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
