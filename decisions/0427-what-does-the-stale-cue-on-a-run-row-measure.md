---
{
  "id": 427,
  "title": "What does the !! stale cue on a run row measure?",
  "status": "decided",
  "key": "ui.stale-cue",
  "asked_by": "Jon",
  "decided_by": "Jon",
  "created": "2026-11-17",
  "decided_at": "2026-11-17",
  "context": "S227 spec question, 2026-11-17.",
  "options": [
    {
      "label": "last-output",
      "summary": "30 min since the role's last output (the watchdog's last-event time)"
    },
    {
      "label": "role-start",
      "summary": "30 min since the role started"
    }
  ],
  "recommendation": {
    "label": "last-output",
    "reasoning": "drafter's recommendation from the S227 verification"
  },
  "decision": "last-output",
  "rationale": "Owner chose 'Since last output (Recommended)' in an interactive question, relay session 8d711f67, 2026-11-17.",
  "refs": [
    "S227"
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
