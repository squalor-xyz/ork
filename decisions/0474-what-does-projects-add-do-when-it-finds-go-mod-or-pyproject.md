---
{
  "id": 474,
  "title": "What does projects add do when it finds go.mod or pyproject.toml?",
  "status": "decided",
  "key": "projects.add-setup",
  "asked_by": "Jon",
  "decided_by": "Jon",
  "created": "2026-11-18",
  "decided_at": "2026-11-18",
  "context": "Plan and doctor already warn and suggest a preset (internal/engine/setup.go:90-149).",
  "options": [
    {
      "label": "warn",
      "summary": "Print the setup warnings with fix commands; write nothing"
    },
    {
      "label": "apply",
      "summary": "Apply the matching preset when exactly one marker is found"
    },
    {
      "label": "ask",
      "summary": "Ask y/N"
    }
  ],
  "recommendation": {
    "label": "warn",
    "reasoning": "Recommended in the interactive question."
  },
  "decision": "warn",
  "rationale": "Owner chose 'Warn with fix commands (Recommended)' in an interactive question, relay session 8d711f67, 2026-11-18.",
  "refs": [
    "S339"
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
