---
{
  "id": 120,
  "title": "How does the TUI switch between Overview, Roadmap and Decisions?",
  "status": "decided",
  "key": "ui.views",
  "asked_by": "claude",
  "decided_by": "jon",
  "created": "2026-10-20",
  "decided_at": "2026-10-20",
  "context": "The tab bar is static text (internal/tui/view.go:167-171); S12 adds the screens.",
  "options": [
    {
      "label": "tab-switches-views",
      "summary": "Tab and shift+tab switch views; left/right and 1-9 switch projects"
    }
  ],
  "recommendation": {
    "label": "tab-switches-views",
    "reasoning": "Owner answer 2026-10-20 in the roadmap review."
  },
  "decision": "tab-switches-views",
  "rationale": "Owner 2026-10-20: left/right and 1-9 already switch projects. This changes S57's tab binding (tab was next project).",
  "refs": [
    "S12,S57"
  ],
  "history": [
    {
      "date": "2026-10-20",
      "event": "ask"
    },
    {
      "date": "2026-10-20",
      "event": "decide"
    }
  ]
}
---
