---
{
  "id": 358,
  "title": "Does notify_command name only an executable, or may it include configured arguments?",
  "status": "decided",
  "key": "notify.command-syntax",
  "asked_by": "Jon",
  "decided_by": "Jon",
  "created": "2026-11-15",
  "decided_at": "2026-11-15",
  "context": "S311 implementer question (run ork-20261115-033114-333e).",
  "options": [
    {
      "label": "executable-only",
      "summary": "treat the whole CMD as a program name or path, spaces included; pass no arguments"
    },
    {
      "label": "configured-argv",
      "summary": "parse quoting and backslash escapes for static arguments, append no event arguments, a shell only when CMD names one"
    }
  ],
  "recommendation": {
    "label": "executable-only",
    "reasoning": "follows the decided no-arguments rule; wrapper scripts cover the rest without a command parser"
  },
  "decision": "executable-only",
  "rationale": "Owner chose 'Executable only (Recommended)' in an interactive question, relay session 8d711f67, 2026-11-15.",
  "refs": [
    "S311"
  ],
  "history": [
    {
      "date": "2026-11-15",
      "event": "ask"
    },
    {
      "date": "2026-11-15",
      "event": "decide"
    }
  ]
}
---
