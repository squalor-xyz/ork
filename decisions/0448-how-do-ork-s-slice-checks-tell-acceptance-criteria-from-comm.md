---
{
  "id": 448,
  "title": "How do ork's slice checks tell acceptance criteria from commands in a Check section?",
  "status": "decided",
  "key": "checks.acceptance-criteria",
  "asked_by": "Jon",
  "decided_by": "Jon",
  "created": "2026-11-17",
  "decided_at": "2026-11-17",
  "context": "S387 paused 2026-11-17: slice checks tried to run 'friction' from a criteria bullet.",
  "options": [
    {
      "label": "skip-criteria",
      "summary": "bullets after an Acceptance criteria: line, up to Commands:, are never run"
    },
    {
      "label": "commands-only",
      "summary": "when a Commands: line exists, run only its bullets"
    },
    {
      "label": "leading-span",
      "summary": "run a bullet only if it starts with a backticked command"
    }
  ],
  "recommendation": {
    "label": "skip-criteria",
    "reasoning": "criteria keep code formatting; old specs unchanged"
  },
  "decision": "skip-criteria",
  "rationale": "Owner chose 'Skip criteria block (Recommended)' in an interactive question, relay session 8d711f67, 2026-11-17.",
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
