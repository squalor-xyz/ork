---
{
  "id": 687,
  "title": "Where may a claude-local UI pass write?",
  "status": "decided",
  "key": "ui.renders-dir-grant-claude-local",
  "asked_by": "Jon",
  "decided_by": "Jon",
  "created": "2026-11-27",
  "decided_at": "2026-11-27",
  "context": "S667 spec draft.",
  "options": [
    {
      "label": "temp-dir",
      "summary": "the same write directory as cloud claude (req.TempDir, runner.go:49-52)"
    },
    {
      "label": "renders-only",
      "summary": "TempDir/renders only"
    }
  ],
  "recommendation": {
    "label": "temp-dir",
    "reasoning": "Recommended in the interactive question."
  },
  "decision": "temp-dir",
  "rationale": "Owner chose 'Temp directory' in an interactive question, relay session 8d711f67, 2026-11-27.",
  "refs": [
    "S667"
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
