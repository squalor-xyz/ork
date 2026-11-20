---
{
  "id": 509,
  "title": "What does the CLI render test assert about width?",
  "status": "decided",
  "key": "ui.render-flag-width",
  "asked_by": "Jon",
  "decided_by": "Jon",
  "created": "2026-11-19",
  "decided_at": "2026-11-19",
  "context": "S475 spec check 2026-11-19: no internal/cli view takes a width; only wrapLines wraps at 80 (internal/cli/prompt.go:93).",
  "options": [
    {
      "label": "as-printed",
      "summary": "Files hold what the command prints; assert names, no ANSI and non-empty text"
    },
    {
      "label": "max80",
      "summary": "Assert no line wider than 80 on fixtures that fit"
    }
  ],
  "recommendation": {
    "label": "as-printed",
    "reasoning": "Recommended in the interactive question."
  },
  "decision": "as-printed",
  "rationale": "Owner chose 'As printed (Recommended)' in an interactive question, relay session 8d711f67, 2026-11-19.",
  "refs": [
    "S475"
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
