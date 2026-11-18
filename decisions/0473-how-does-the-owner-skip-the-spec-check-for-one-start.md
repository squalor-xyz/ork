---
{
  "id": 473,
  "title": "How does the owner skip the spec check for one start?",
  "status": "decided",
  "key": "cli.spec-check-skip",
  "asked_by": "Jon",
  "decided_by": "Jon",
  "created": "2026-11-18",
  "decided_at": "2026-11-18",
  "context": "The CLI always runs the check (internal/cli/wave.go:381, internal/cli/run.go:143).",
  "options": [
    {
      "label": "flag",
      "summary": "--spec-check off on ork run and ork wave start, CLI only"
    },
    {
      "label": "flag-tui",
      "summary": "The flag plus a TUI confirm toggle"
    },
    {
      "label": "project",
      "summary": "A per-project setting"
    }
  ],
  "recommendation": {
    "label": "flag",
    "reasoning": "Recommended in the interactive question."
  },
  "decision": "flag",
  "rationale": "Owner chose '--spec-check off flag (Recommended)' in an interactive question, relay session 8d711f67, 2026-11-18.",
  "refs": [
    "S338"
  ],
  "history": [
    {
      "date": "2026-11-18",
      "event": "ask"
    },
    {
      "date": "2026-11-18",
      "event": "decide"
    }
  ]
}
---
