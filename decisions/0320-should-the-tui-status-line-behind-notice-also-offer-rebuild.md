---
{
  "id": 320,
  "title": "Should the TUI status-line behind notice also offer --rebuild when a build command is set?",
  "status": "decided",
  "key": "selfhost.rebuild-tui-status",
  "asked_by": "claude",
  "decided_by": "Jon",
  "via": "interactive question, relay session 8d711f67, 2026-11-11",
  "created": "2026-11-11",
  "decided_at": "2026-11-11",
  "context": "S315 run ork-20261111-215046-00fa review question, 2026-11-11.",
  "options": [
    {
      "label": "now",
      "summary": "pass BuildCommand at internal/tui/view.go:606 in S315 and add view.go to its Files"
    },
    {
      "label": "later",
      "summary": "file a follow-up slice for the TUI status-line notice"
    }
  ],
  "recommendation": {
    "label": "now",
    "reasoning": "Otherwise the status line beside the landed line shows a restart hint that keeps the old binary."
  },
  "decision": "now",
  "rationale": "Owner picked now (Recommended); recorded by the agent as the owner's delegate (adj 316).",
  "refs": [
    "S315"
  ],
  "history": [
    {
      "date": "2026-11-11",
      "event": "ask"
    },
    {
      "date": "2026-11-11",
      "event": "decide"
    }
  ]
}
---
