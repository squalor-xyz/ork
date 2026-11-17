---
{
  "id": 434,
  "title": "Where does the FIFO-safe report read helper live?",
  "status": "decided",
  "key": "code.safe-report-read",
  "asked_by": "Jon",
  "decided_by": "Jon",
  "created": "2026-11-17",
  "decided_at": "2026-11-17",
  "context": "S227 spec question, 2026-11-17.",
  "options": [
    {
      "label": "clirun",
      "summary": "internal/clirun"
    },
    {
      "label": "new-package",
      "summary": "a new package"
    },
    {
      "label": "copy",
      "summary": "a copy per package"
    }
  ],
  "recommendation": {
    "label": "clirun",
    "reasoning": "drafter's recommendation from the S227 verification"
  },
  "decision": "clirun",
  "rationale": "Owner chose 'internal/clirun (Recommended)' in an interactive question, relay session 8d711f67, 2026-11-17.",
  "refs": [
    "S227"
  ],
  "history": [
    {
      "date": "2026-11-17",
      "event": "ask"
    },
    {
      "date": "2026-11-17",
      "event": "decide"
    }
  ]
}
---
