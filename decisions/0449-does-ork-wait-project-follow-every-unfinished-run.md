---
{
  "id": 449,
  "title": "Does ork wait --project follow every unfinished run?",
  "status": "decided",
  "key": "cli.wait-project",
  "asked_by": "Jon",
  "decided_by": "Jon",
  "created": "2026-11-17",
  "decided_at": "2026-11-17",
  "context": "Automation slice question (owner rule: deterministic work in code), 2026-11-17.",
  "options": [
    {
      "label": "all-runs",
      "summary": "watch all unfinished runs; return when any changes"
    },
    {
      "label": "one-run",
      "summary": "explicit run ids only"
    }
  ],
  "recommendation": {
    "label": "all-runs",
    "reasoning": "drafter's recommendation, automation slices"
  },
  "decision": "all-runs",
  "rationale": "Owner chose 'Yes, supersede 203 (Rec)' in an interactive question, relay session 8d711f67, 2026-11-17.",
  "supersedes": [
    203
  ],
  "history": [
    {
      "date": "2026-11-17",
      "event": "ask"
    },
    {
      "date": "2026-11-17",
      "event": "decide"
    },
    {
      "date": "2026-11-17",
      "event": "supersede",
      "note": "supersedes 203"
    }
  ]
}
---
