---
{
  "id": 649,
  "title": "What file names does the TUI -renders DIR write, and what is the print mode's flag?",
  "status": "decided",
  "key": "roles.ui-pass-tui-render-files",
  "asked_by": "Jon",
  "decided_by": "Jon",
  "created": "2026-11-27",
  "decided_at": "2026-11-27",
  "context": "S538 spec draft.",
  "options": [
    {
      "label": "per-size-render-print",
      "summary": "NAME-80x24.txt, NAME-120x40.txt and NAME-ascii.txt; a separate -render-print bool prints fixture text to stdout"
    },
    {
      "label": "dash-dir",
      "summary": "-renders - means stdout"
    },
    {
      "label": "one-file",
      "summary": "one NAME.txt"
    }
  ],
  "recommendation": {
    "label": "per-size-render-print",
    "reasoning": "Recommended in the interactive question."
  },
  "decision": "per-size-render-print",
  "rationale": "Owner chose 'Per-size files + -render-print' in an interactive question, relay session 8d711f67, 2026-11-27.",
  "refs": [
    "S538"
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
