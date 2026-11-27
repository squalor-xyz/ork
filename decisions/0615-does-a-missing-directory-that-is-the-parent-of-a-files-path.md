---
{
  "id": 615,
  "title": "Does a missing directory that is the parent of a Files path count as a Files-created path?",
  "status": "decided",
  "key": "checks.path-arguments-files-parents",
  "asked_by": "Jon",
  "decided_by": "Jon",
  "created": "2026-11-27",
  "decided_at": "2026-11-27",
  "context": "S615 spec draft.",
  "options": [
    {
      "label": "exempt-parents",
      "summary": "exempt when it equals a Files path or is a directory holding one"
    },
    {
      "label": "exact-only",
      "summary": "exact Files match only"
    }
  ],
  "recommendation": {
    "label": "exempt-parents",
    "reasoning": "Recommended in the interactive question."
  },
  "decision": "exempt-parents",
  "rationale": "Owner chose 'Exempt parents' in an interactive question, relay session 8d711f67, 2026-11-27.",
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
