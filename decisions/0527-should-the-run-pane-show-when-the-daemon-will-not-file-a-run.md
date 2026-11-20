---
{
  "id": 527,
  "title": "Should the run pane show when the daemon will not file a run answer it marks [x]?",
  "status": "decided",
  "key": "ui.run-answer-filing-state",
  "asked_by": "Jon",
  "decided_by": "Jon",
  "created": "2026-11-20",
  "decided_at": "2026-11-20",
  "context": "S355 reviewer: with no owner or file-answers off the daemon silently files nothing.",
  "options": [
    {
      "label": "notice",
      "summary": "The daemon returns a notice when file-answers off stops filing a keyed answer"
    },
    {
      "label": "show-state",
      "summary": "The TUI shows why filing is off before submit"
    },
    {
      "label": "keep",
      "summary": "Leave as is"
    }
  ],
  "recommendation": {
    "label": "notice",
    "reasoning": "Recommended in the interactive question."
  },
  "decision": "notice",
  "rationale": "Owner chose 'Daemon returns a notice (Recommended)' in an interactive question, relay session 8d711f67, 2026-11-20.",
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
