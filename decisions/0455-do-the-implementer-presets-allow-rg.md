---
{
  "id": 455,
  "title": "Do the implementer presets allow rg?",
  "status": "superseded",
  "key": "launch.allowlist-rg",
  "asked_by": "Jon",
  "decided_by": "Jon",
  "created": "2026-11-17",
  "decided_at": "2026-11-17",
  "context": "rg is missing from ImplementerPreset (internal/engine/perm.go:51-66), so every rg Check line is orchestrator-only. Extends adj 75.",
  "options": [
    {
      "label": "allow",
      "summary": "Add Bash(rg *) to the go and python presets (read-only search)"
    },
    {
      "label": "orchestrator",
      "summary": "rg Check lines stay orchestrator-only"
    },
    {
      "label": "no-rg",
      "summary": "Specs use a Go test or Grep instead of rg lines"
    }
  ],
  "recommendation": {
    "label": "allow",
    "reasoning": "rg is read-only and the project's search tool; rg Check lines keep landing as orchestrator-only questions"
  },
  "decision": "allow",
  "rationale": "Owner chose 'Allow Bash(rg *)' in an interactive question, relay session 8d711f67, 2026-11-17.",
  "superseded_by": 457,
  "history": [
    {
      "date": "2026-11-17",
      "event": "ask"
    },
    {
      "date": "2026-11-17",
      "event": "decide"
    },
    {
      "date": "2026-11-17",
      "event": "supersede",
      "note": "by 457"
    }
  ]
}
---
