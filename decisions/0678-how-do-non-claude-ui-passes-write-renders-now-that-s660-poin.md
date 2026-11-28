---
{
  "id": 678,
  "title": "How do non-claude UI passes write renders now that S660 points render commands at the renders directory?",
  "status": "decided",
  "key": "ui.renders-dir-grant-vendors",
  "asked_by": "Jon",
  "decided_by": "Jon",
  "created": "2026-11-27",
  "decided_at": "2026-11-27",
  "context": "S660 landed; only claude's UI pass may write the renders directory (internal/daemon/runner.go:44; S648 gates the sentence on claude).",
  "options": [
    {
      "label": "grant-all",
      "summary": "give each vendor's UI pass write access to the renders directory, like claude's grant"
    },
    {
      "label": "temp-dir",
      "summary": "non-claude writes to its temp dir and ork collects the files"
    },
    {
      "label": "claude-only",
      "summary": "only claude UI passes render"
    }
  ],
  "recommendation": {
    "label": "grant-all",
    "reasoning": "Recommended in the interactive question."
  },
  "decision": "grant-all",
  "rationale": "Owner chose 'Grant renders/ to every UI vendor' in an interactive question, relay session 8d711f67, 2026-11-27.",
  "refs": [
    "S660"
  ],
  "history": [
    {
      "date": "2026-11-27",
      "event": "ask"
    },
    {
      "date": "2026-11-27",
      "event": "decide"
    }
  ]
}
---
