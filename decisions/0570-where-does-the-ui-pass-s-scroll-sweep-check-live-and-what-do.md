---
{
  "id": 570,
  "title": "Where does the UI pass's scroll-sweep check live, and what does it cover?",
  "status": "decided",
  "key": "ui.scroll-sweep-home",
  "asked_by": "Jon",
  "decided_by": "Jon",
  "created": "2026-11-25",
  "decided_at": "2026-11-25",
  "context": "S531: page to the end and back at several heights and assert every line appears.",
  "options": [
    {
      "label": "in-tree-test",
      "summary": "permanent internal/tui TestScrollSweep over every scrollable view"
    },
    {
      "label": "overlay-sweep",
      "summary": "extend the adj 507 overlay recipe, nothing in the tree"
    },
    {
      "label": "pane-only",
      "summary": "in-tree sweep for internal/tui/pane.go only"
    }
  ],
  "recommendation": {
    "label": "in-tree-test",
    "reasoning": "Recommended in the interactive question."
  },
  "decision": "in-tree-test",
  "rationale": "Owner chose 'In-tree test' in an interactive question, relay session 8d711f67, 2026-11-25.",
  "refs": [
    "S531"
  ],
  "history": [
    {
      "date": "2026-11-25",
      "event": "ask"
    },
    {
      "date": "2026-11-25",
      "event": "decide"
    }
  ]
}
---
