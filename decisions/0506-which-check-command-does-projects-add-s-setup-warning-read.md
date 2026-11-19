---
{
  "id": 506,
  "title": "Which check command does projects add's setup warning read?",
  "status": "decided",
  "key": "projects.add-check-source",
  "asked_by": "Jon",
  "decided_by": "Jon",
  "created": "2026-11-19",
  "decided_at": "2026-11-19",
  "context": "S339 spec check 2026-11-19: ork doctor and the plan warn from the effective command (slicer config check_command first, internal/cli/doctor.go:130).",
  "options": [
    {
      "label": "effective",
      "summary": "Read slicer config check_command as ork doctor does; fall back to the registry on a read failure"
    },
    {
      "label": "registry",
      "summary": "Registry only"
    }
  ],
  "recommendation": {
    "label": "effective",
    "reasoning": "Recommended in the interactive question."
  },
  "decision": "effective",
  "rationale": "Owner chose 'Effective command (Recommended)' in an interactive question, relay session 8d711f67, 2026-11-19.",
  "refs": [
    "S339"
  ],
  "history": [
    {
      "date": "2026-11-19",
      "event": "ask"
    },
    {
      "date": "2026-11-19",
      "event": "decide"
    }
  ]
}
---
