---
{
  "id": 75,
  "title": "What allowlist does the implementer get when the registry has no permissions entry?",
  "status": "decided",
  "key": "launch.default-allowlist",
  "asked_by": "claude",
  "decided_by": "jon",
  "created": "2026-10-19",
  "decided_at": "2026-10-19",
  "context": "launch.permissions decided a per-project, per-role allowlist in the registry.",
  "options": [
    {
      "label": "built-in",
      "summary": "A narrow built-in default the registry overrides"
    },
    {
      "label": "refuse",
      "summary": "Refuse to launch until the registry has one"
    }
  ],
  "decision": "built-in",
  "rationale": "Owner 2026-10-19: built-in default (Read, Grep, Glob, Edit, Write, Bash(slicer *), Bash(git status*|diff*|log*), Bash(check command)); the registry overrides it; ork run shows it.",
  "refs": [
    "S04"
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
