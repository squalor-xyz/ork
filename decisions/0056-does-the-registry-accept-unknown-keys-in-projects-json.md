---
{
  "id": 56,
  "title": "Does the registry accept unknown keys in projects.json?",
  "status": "decided",
  "key": "registry.unknown-keys",
  "asked_by": "claude",
  "decided_by": "jon",
  "created": "2026-10-18",
  "decided_at": "2026-10-18",
  "context": "S02 Load decodes strictly after checking version.",
  "options": [
    {
      "label": "strict",
      "summary": "unknown key is an error"
    },
    {
      "label": "ignore",
      "summary": "unknown keys are ignored"
    }
  ],
  "decision": "strict",
  "rationale": "Owner 2026-10-19: An unknown key is an error naming it; new fields come with a version bump.",
  "refs": [
    "S02"
  ],
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
