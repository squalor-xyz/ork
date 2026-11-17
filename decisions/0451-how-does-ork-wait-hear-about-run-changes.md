---
{
  "id": 451,
  "title": "How does ork wait hear about run changes?",
  "status": "decided",
  "key": "cli.wait-signal",
  "asked_by": "Jon",
  "decided_by": "Jon",
  "created": "2026-11-17",
  "decided_at": "2026-11-17",
  "context": "Automation slice question (owner rule: deterministic work in code), 2026-11-17.",
  "options": [
    {
      "label": "snapshot-changed",
      "summary": "the existing notice with an optional run list"
    },
    {
      "label": "new-notice",
      "summary": "a run.changed notice"
    },
    {
      "label": "poll",
      "summary": "client-side polling"
    }
  ],
  "recommendation": {
    "label": "snapshot-changed",
    "reasoning": "drafter's recommendation, automation slices"
  },
  "decision": "snapshot-changed",
  "rationale": "Owner chose 'snapshot.changed (Rec)' in an interactive question, relay session 8d711f67, 2026-11-17.",
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
