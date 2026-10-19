---
{
  "id": 70,
  "title": "Must a path given to ork projects add exist?",
  "status": "decided",
  "key": "projects.add-path",
  "asked_by": "claude",
  "decided_by": "jon",
  "created": "2026-10-19",
  "decided_at": "2026-10-19",
  "context": "S03 refused only relative paths and duplicate names.",
  "options": [
    {
      "label": "existing-dir",
      "summary": "Refuse a path that is not an existing directory"
    },
    {
      "label": "any-absolute",
      "summary": "Accept any absolute path"
    }
  ],
  "decision": "existing-dir",
  "rationale": "Owner 2026-10-19: the path must exist; refuse one that does not.",
  "refs": [
    "S03"
  ],
  "history": [
    {
      "date": "2026-10-19",
      "event": "ask"
    },
    {
      "date": "2026-10-19",
      "event": "decide"
    }
  ]
}
---
