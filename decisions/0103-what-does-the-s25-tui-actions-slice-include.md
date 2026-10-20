---
{
  "id": 103,
  "title": "What does the S25 TUI actions slice include?",
  "status": "superseded",
  "key": "ui.launch-scope",
  "asked_by": "claude",
  "decided_by": "jon",
  "created": "2026-10-20",
  "decided_at": "2026-10-20",
  "context": "S25 specs launch, approve and stop; S44 added ork retry. The spec left open how a launch picks its slice and the two y/n inputs.",
  "options": [
    {
      "label": "launch-retry",
      "summary": "approve, stop, retry, and launch from the ready-slice list with the two y/n inputs and a confirm",
      "tradeoffs": "biggest; closes the run loop in the TUI"
    },
    {
      "label": "approve-stop",
      "summary": "approve and stop only; launch in a later slice",
      "tradeoffs": "smallest; launch needs its own interview"
    },
    {
      "label": "as-specced",
      "summary": "launch, approve, stop; no retry key",
      "tradeoffs": "retry waits for S26"
    }
  ],
  "recommendation": {
    "label": "launch-retry",
    "reasoning": "Closes the loop in the one app, per the owner's goal."
  },
  "decision": "launch-retry",
  "rationale": "Owner 2026-10-20: launch on a ready slice plus a retry key.",
  "superseded_by": 107,
  "refs": [
    "S25",
    "S44"
  ],
  "history": [
    {
      "date": "2026-10-20",
      "event": "ask"
    },
    {
      "date": "2026-10-20",
      "event": "decide"
    },
    {
      "date": "2026-10-20",
      "event": "supersede",
      "note": "by 107"
    }
  ]
}
---
