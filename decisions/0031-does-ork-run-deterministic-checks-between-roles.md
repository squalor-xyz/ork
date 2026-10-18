---
{
  "id": 31,
  "title": "Does ork run deterministic checks between roles?",
  "status": "decided",
  "key": "checks.deterministic",
  "asked_by": "claude",
  "decided_by": "jon",
  "created": "2026-10-18",
  "decided_at": "2026-10-18",
  "context": "N6: deterministic checks before any model review.",
  "options": [
    {
      "label": "yes",
      "summary": "Check command and slicer check before review"
    },
    {
      "label": "no",
      "summary": "Roles run tests"
    }
  ],
  "decision": "yes",
  "rationale": "Owner interview 2026-10-18: ork runs the project check command and slicer check after the implementer; red goes back without a review.",
  "history": [
    {
      "date": "2026-10-18",
      "event": "ask"
    },
    {
      "date": "2026-10-18",
      "event": "decide"
    }
  ]
}
---
