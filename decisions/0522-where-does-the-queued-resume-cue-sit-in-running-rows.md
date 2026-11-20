---
{
  "id": 522,
  "title": "Where does the queued-resume cue sit in Running rows?",
  "status": "decided",
  "key": "ui.resume-queued-cue",
  "asked_by": "Jon",
  "decided_by": "Jon",
  "created": "2026-11-20",
  "decided_at": "2026-11-20",
  "context": "S449 UI pass: the cue in the heard cell widens the shared heard column, so rows lose their state at 80x24 (overview.go:162,1010-1012).",
  "options": [
    {
      "label": "role-cell",
      "summary": "Show resume queued in place of paused in the role cell; keep wait in heard"
    },
    {
      "label": "own-width",
      "summary": "Keep it in heard, outside the shared width"
    },
    {
      "label": "accept",
      "summary": "Keep as built"
    }
  ],
  "recommendation": {
    "label": "role-cell",
    "reasoning": "Recommended in the interactive question."
  },
  "decision": "role-cell",
  "rationale": "Owner chose 'Replace 'paused' (Recommended)' in an interactive question, relay session 8d711f67, 2026-11-20.",
  "refs": [
    "S449"
  ],
  "history": [
    {
      "date": "2026-11-20",
      "event": "ask"
    },
    {
      "date": "2026-11-20",
      "event": "decide"
    }
  ]
}
---
