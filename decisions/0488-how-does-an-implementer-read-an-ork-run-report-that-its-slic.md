---
{
  "id": 488,
  "title": "How does an implementer read an ork run report that its slice names?",
  "status": "decided",
  "key": "roles.implementer-run-reports",
  "asked_by": "Jon",
  "decided_by": "Jon",
  "created": "2026-11-18",
  "decided_at": "2026-11-18",
  "context": "Run ork-20261117-234004-9e8e; the implementer presets (adj 75, adj 94) do not allow ork show.",
  "options": [
    {
      "label": "allow",
      "summary": "Add Bash(ork show*) to both implementer presets"
    },
    {
      "label": "copy",
      "summary": "ork copies the named reports into the role's temp dir"
    },
    {
      "label": "spec",
      "summary": "The spec writer copies them in before the run"
    }
  ],
  "recommendation": {
    "label": "allow",
    "reasoning": "Recommended in the interactive question."
  },
  "decision": "allow",
  "rationale": "Owner chose 'Allow ork show (Recommended)' in an interactive question, relay session 8d711f67, 2026-11-18.",
  "refs": [
    "S417"
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
