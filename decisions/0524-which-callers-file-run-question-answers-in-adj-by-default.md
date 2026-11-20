---
{
  "id": 524,
  "title": "Which callers file run-question answers in adj by default?",
  "status": "decided",
  "key": "owner.run-answers-scope",
  "asked_by": "Jon",
  "decided_by": "Jon",
  "created": "2026-11-20",
  "decided_at": "2026-11-20",
  "context": "adj 327 puts the default and the owner resolution in the daemon for run.answer.",
  "options": [
    {
      "label": "daemon-noadj",
      "summary": "The daemon files by default for every caller from the owner setting; a per-call no_adj field lets ctrl+a opt out"
    },
    {
      "label": "tui-only",
      "summary": "Only the TUI pane files by default"
    },
    {
      "label": "daemon-default",
      "summary": "The daemon decides alone; no per-call flag"
    }
  ],
  "recommendation": {
    "label": "daemon-noadj",
    "reasoning": "Recommended in the interactive question."
  },
  "decision": "daemon-noadj",
  "rationale": "Owner chose 'Every caller, daemon default (Recommended)' in an interactive question, relay session 8d711f67, 2026-11-20.",
  "refs": [
    "S355"
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
