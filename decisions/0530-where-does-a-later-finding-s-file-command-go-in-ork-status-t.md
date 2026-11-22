---
{
  "id": 530,
  "title": "Where does a later finding's file command go in ork status text?",
  "status": "decided",
  "key": "ui.status-finding-file-line",
  "asked_by": "Jon",
  "decided_by": "Jon",
  "created": "2026-11-21",
  "decided_at": "2026-11-21",
  "context": "S240 UI pass: the inline '(file it: ork file RUN N)' runs past 80 columns with real ork ids; adj 526 confirmed the inline form.",
  "options": [
    {
      "label": "own-line",
      "summary": "Print ' file it: ork file RUN N' on an indented line under the finding"
    },
    {
      "label": "keep",
      "summary": "Keep it inline"
    },
    {
      "label": "short-hint",
      "summary": "Shorten the inline hint"
    }
  ],
  "recommendation": {
    "label": "own-line",
    "reasoning": "Recommended in the interactive question."
  },
  "decision": "own-line",
  "rationale": "Owner chose 'Own indented line (Recommended)' in an interactive question, relay session 8d711f67, 2026-11-21.",
  "narrows": [
    526
  ],
  "refs": [
    "S240"
  ],
  "history": [
    {
      "date": "2026-11-21",
      "event": "ask"
    },
    {
      "date": "2026-11-21",
      "event": "decide"
    },
    {
      "date": "2026-11-21",
      "event": "narrow",
      "note": "narrows 526"
    }
  ]
}
---
