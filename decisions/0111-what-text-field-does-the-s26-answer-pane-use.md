---
{
  "id": 111,
  "title": "What text field does the S26 answer pane use?",
  "status": "decided",
  "key": "ui.text-field",
  "asked_by": "claude",
  "decided_by": "jon",
  "created": "2026-10-20",
  "decided_at": "2026-10-20",
  "context": "S26 orchestrator: Bubbles textinput (charm.land/bubbles/v2@v2.2.1) imports github.com/atotto/clipboard, a module outside the approved set (Bubble Tea, Lip Gloss, Bubbles only); go build fails without a go.sum entry.",
  "options": [
    {
      "label": "own-field",
      "summary": "a small stdlib line editor in internal/tui",
      "tradeoffs": "no dependency; a little code to own and test"
    },
    {
      "label": "approve-clipboard",
      "summary": "approve github.com/atotto/clipboard after a license check and use textinput",
      "tradeoffs": "less code; a new module for an unused clipboard feature"
    }
  ],
  "recommendation": {
    "label": "own-field",
    "reasoning": "Standard library first; the field is small."
  },
  "decision": "own-field",
  "rationale": "Owner 2026-10-20: write a small stdlib text field in internal/tui; do not add github.com/atotto/clipboard or use Bubbles textinput.",
  "refs": [
    "S26",
    "stack.go-deps"
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
