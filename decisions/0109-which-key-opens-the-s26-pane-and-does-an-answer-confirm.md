---
{
  "id": 109,
  "title": "Which key opens the S26 pane, and does an answer confirm?",
  "status": "decided",
  "key": "ui.answer-pane-keys",
  "asked_by": "claude",
  "decided_by": "jon",
  "created": "2026-10-20",
  "decided_at": "2026-10-20",
  "context": "S26 preflight: Enter on a Needs-you row switches to the row's project (internal/tui/model.go:346). #98 confirms launch and stop only.",
  "options": [
    {
      "label": "enter-engine",
      "summary": "Enter opens the pane on engine rows; other rows keep switching project",
      "tradeoffs": "matches the spec; one overloaded key"
    },
    {
      "label": "new-key",
      "summary": "a new key opens the pane; Enter unchanged",
      "tradeoffs": "no behaviour change; extra key"
    },
    {
      "label": "answer-no-confirm",
      "summary": "Enter in the text field submits",
      "tradeoffs": "fewer keys"
    },
    {
      "label": "answer-confirm",
      "summary": "y/n after Enter",
      "tradeoffs": "consistent with stop; slower"
    }
  ],
  "recommendation": {
    "label": "enter-engine",
    "reasoning": "Matches the spec and the owner's one-app goal; typing an answer is deliberate."
  },
  "decision": "enter-engine",
  "rationale": "Owner 2026-10-20: Enter opens the pane on engine run rows and other rows keep switching project; submitting an answer needs no y/n, the pane shows the command and Enter in the text field submits (an empty answer is refused).",
  "refs": [
    "S26",
    "98",
    "99"
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
