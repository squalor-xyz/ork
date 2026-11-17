---
{
  "id": 458,
  "title": "What does ork retry do with a committed run whose landing was blocked?",
  "status": "decided",
  "key": "land.retry-relands",
  "asked_by": "Jon",
  "decided_by": "Jon",
  "created": "2026-11-17",
  "decided_at": "2026-11-17",
  "context": "NotResumable refuses a committed run (internal/engine/resume.go:24-25); landBlocked stops after one land fix (internal/engine/land.go:262-266). S387 had to be landed by hand.",
  "options": [
    {
      "label": "land-only",
      "summary": "Merge main, rerun the checks, land if green; stop again if red"
    },
    {
      "label": "land-plus-fix",
      "summary": "Reset the land fix count, so a red re-check gets one more implementer fix round"
    },
    {
      "label": "rounds",
      "summary": "--rounds N grants N land fix rounds, default 0"
    }
  ],
  "recommendation": {
    "label": "land-only",
    "reasoning": "the usual cause is on main"
  },
  "decision": "land-plus-fix",
  "rationale": "Owner chose 'Landing + 1 fix round' in an interactive question, relay session 8d711f67, 2026-11-17.",
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
