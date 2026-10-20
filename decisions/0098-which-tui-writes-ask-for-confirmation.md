---
{
  "id": 98,
  "title": "Which TUI writes ask for confirmation?",
  "status": "decided",
  "key": "ui.confirm",
  "asked_by": "claude",
  "decided_by": "jon",
  "created": "2026-10-20",
  "decided_at": "2026-10-20",
  "context": "dashboard.md says launch, approve and commit, and stop confirm. Owner chose on 2026-10-20 to not confirm approve.",
  "options": [
    {
      "label": "launch-stop",
      "summary": "launch and stop confirm; approve runs on one key"
    },
    {
      "label": "all",
      "summary": "launch, approve and stop confirm"
    }
  ],
  "recommendation": {
    "label": "all"
  },
  "decision": "launch-stop",
  "rationale": "Owner 2026-10-20: no confirm for approve; launch and stop confirm. Confirm style for those is left to the slice (the S21 y/n prompts are the model). Refines docs/dashboard.md principle 'Show the command'.",
  "refs": [
    "S23"
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
