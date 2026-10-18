---
{
  "id": 32,
  "title": "Where does a project's check command come from?",
  "status": "decided",
  "key": "project.check-command",
  "asked_by": "claude",
  "decided_by": "jon",
  "created": "2026-10-18",
  "decided_at": "2026-10-18",
  "context": "slicer config has no check-command key (checked 2026-10-18 on four projects).",
  "options": [
    {
      "label": "slicer-then-registry",
      "summary": "slicer key, registry fallback"
    },
    {
      "label": "wait",
      "summary": "Require the slicer key"
    }
  ],
  "decision": "slicer-then-registry",
  "rationale": "Owner interview 2026-10-18: file a slicer item for a check_command key; until it ships ork uses its registry value, and prefers slicer's once it exists.",
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
