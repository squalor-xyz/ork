---
{
  "id": 413,
  "title": "When does ork file or ask about a tool feedback entry?",
  "status": "decided",
  "key": "feedback.when",
  "asked_by": "Jon",
  "decided_by": "Jon",
  "created": "2026-11-17",
  "decided_at": "2026-11-17",
  "context": "S320 spec question, 2026-11-17.",
  "options": [
    {
      "label": "on-report",
      "summary": "as each role reports it; failures never stop the run"
    },
    {
      "label": "after-pass",
      "summary": "only for runs that pass the gate"
    },
    {
      "label": "run-end",
      "summary": "once per run at the end"
    }
  ],
  "recommendation": {
    "label": "on-report",
    "reasoning": "drafter's recommendation from the S320 spec draft"
  },
  "decision": "on-report",
  "rationale": "Owner chose 'On each report (Recommended)' in an interactive question, relay session 8d711f67, 2026-11-17.",
  "refs": [
    "S320"
  ],
  "history": [
    {
      "date": "2026-11-17",
      "event": "ask"
    },
    {
      "date": "2026-11-17",
      "event": "decide"
    }
  ]
}
---
