---
{
  "id": 525,
  "title": "How long does ctrl+a keep filing off, and does file-answers gate run answers?",
  "status": "decided",
  "key": "owner.run-answers-off-switch",
  "asked_by": "Jon",
  "decided_by": "Jon",
  "created": "2026-11-20",
  "decided_at": "2026-11-20",
  "context": "docs/dashboard.md:1154 resets the pane on reopen; file-answers (S314) says file answers.",
  "options": [
    {
      "label": "per-pane",
      "summary": "ctrl+a turns filing off for that pane only; file-answers off also stops run-answer filing"
    },
    {
      "label": "tui-session",
      "summary": "ctrl+a lasts until the TUI quits"
    },
    {
      "label": "ctrl-a-only",
      "summary": "file-answers gates spec answers only"
    }
  ],
  "recommendation": {
    "label": "per-pane",
    "reasoning": "Recommended in the interactive question."
  },
  "decision": "per-pane",
  "rationale": "Owner chose 'Per pane; setting applies (Recommended)' in an interactive question, relay session 8d711f67, 2026-11-20.",
  "refs": [
    "S355"
  ],
  "history": [
    {
      "date": "2026-11-20",
      "event": "ask"
    },
    {
      "date": "2026-11-20",
      "event": "decide"
    }
  ]
}
---
