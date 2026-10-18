---
{
  "id": 29,
  "title": "Where do role prompts live?",
  "status": "decided",
  "key": "roles.prompts",
  "asked_by": "claude",
  "decided_by": "jon",
  "created": "2026-10-18",
  "decided_at": "2026-10-18",
  "context": "agents relay-roles/ files are drifting.",
  "options": [
    {
      "label": "embedded-canonical",
      "summary": "Embedded in ork, canonical"
    },
    {
      "label": "agents-files",
      "summary": "Read from agents relay-roles/"
    },
    {
      "label": "slicer",
      "summary": "Printed by slicer"
    }
  ],
  "decision": "embedded-canonical",
  "rationale": "Owner interview 2026-10-18: embedded in ork and canonical; the agents repo generates its role files from ork's output (ork roles print).",
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
