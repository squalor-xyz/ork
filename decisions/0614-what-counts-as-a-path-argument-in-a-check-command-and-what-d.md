---
{
  "id": 614,
  "title": "What counts as a path argument in a Check command, and what does a missing one do before round 1?",
  "status": "decided",
  "key": "checks.path-arguments",
  "asked_by": "Jon",
  "decided_by": "Jon",
  "created": "2026-11-27",
  "decided_at": "2026-11-27",
  "context": "S615 spec check.",
  "options": [
    {
      "label": "refuse",
      "summary": "a relative word with / or leading ./ is a path (not flags, URLs, quoted patterns, globs, absolute paths); a missing one that is not a Files-created path refuses the start and shows in the spec check"
    },
    {
      "label": "warn",
      "summary": "plan warning only"
    }
  ],
  "recommendation": {
    "label": "refuse",
    "reasoning": "Recommended in the interactive question."
  },
  "decision": "refuse",
  "rationale": "Owner chose 'Refuse the start' in an interactive question, relay session 8d711f67, 2026-11-27.",
  "refs": [
    "S615"
  ],
  "history": [
    {
      "date": "2026-11-27",
      "event": "ask"
    },
    {
      "date": "2026-11-27",
      "event": "decide"
    }
  ]
}
---
