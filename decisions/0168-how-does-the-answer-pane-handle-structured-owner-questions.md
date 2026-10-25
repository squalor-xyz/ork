---
{
  "id": 168,
  "title": "How does the answer pane handle structured owner questions?",
  "status": "open",
  "key": "ui.structured-questions",
  "asked_by": "claude",
  "created": "2026-10-25",
  "context": "S111. Decided #104 (ui.answer-pane-scope: no structured options) and #109 (ui.answer-pane-keys: Enter submits, no confirm) conflict with structured questions. The owner chose option options-confirm in the 2026-10-25 batch spec check (AskUserQuestion); recording that needs the owner's decide and a supersede of #104 and #109 for structured questions only.",
  "options": [
    {
      "label": "options-confirm",
      "summary": "a role's Owner needed block carries a title, 2 to 4 options (label and summary), a recommendation and a reason; the pane lists options by digit and marks the recommended one; a digit opens a confirm bar showing the exact answer (Enter is No, y sends); t switches to free text, where Enter submits as #109 says; esc in free text returns to the options and a second esc closes; one shape for S116 and S120"
    },
    {
      "label": "digit-enter",
      "summary": "a digit selects and Enter submits, no confirm (supersedes #104 only)"
    },
    {
      "label": "confirm-all",
      "summary": "every answer, free text too, asks for confirmation (supersedes #109)"
    }
  ],
  "recommendation": {
    "label": "options-confirm",
    "reasoning": "The exact command is shown before an option is sent, while typing a free answer stays fast."
  },
  "refs": [
    "S111",
    "ui.answer-pane-scope",
    "ui.answer-pane-keys",
    "S116",
    "S120"
  ],
  "history": [
    {
      "date": "2026-10-25",
      "event": "ask"
    }
  ]
}
---
