---
{
  "id": 454,
  "title": "How are Check lines the implementer cannot run handled?",
  "status": "decided",
  "key": "checks.three-way-split",
  "asked_by": "Jon",
  "decided_by": "Jon",
  "created": "2026-11-17",
  "decided_at": "2026-11-17",
  "context": "Automation slice question (owner rule: deterministic work in code), 2026-11-17.",
  "options": [
    {
      "label": "three-way",
      "summary": "implementer, ork-run, orchestrator; ork runs what it can and && chains as parts; the spec check never asks"
    },
    {
      "label": "rewrite",
      "summary": "rewrite lines into implementer-runnable form (adj 280)"
    }
  ],
  "recommendation": {
    "label": "three-way",
    "reasoning": "drafter's recommendation, automation slices"
  },
  "decision": "three-way",
  "rationale": "Owner chose 'Supersede, split 3 ways (Rec)' in an interactive question, relay session 8d711f67, 2026-11-17.",
  "supersedes": [
    280
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
      "note": "supersedes 280"
    }
  ]
}
---
