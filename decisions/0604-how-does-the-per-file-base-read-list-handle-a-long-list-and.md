---
{
  "id": 604,
  "title": "How does the per-file base-read list handle a long list, and a file the base lacks?",
  "status": "decided",
  "key": "base-overlays.other-files-list",
  "asked_by": "Jon",
  "decided_by": "Jon",
  "created": "2026-11-25",
  "decided_at": "2026-11-25",
  "context": "S618 spec check.",
  "options": [
    {
      "label": "cap-mark-new",
      "summary": "reuse the 20-path cap with N more left out; a file not in the base is marked new in this slice, no base version, with no command"
    },
    {
      "label": "no-cap",
      "summary": "list every path"
    },
    {
      "label": "command-for-all",
      "summary": "cap, command for all"
    }
  ],
  "recommendation": {
    "label": "cap-mark-new",
    "reasoning": "Recommended in the interactive question."
  },
  "decision": "cap-mark-new",
  "rationale": "Owner chose 'Cap 20, mark new' in an interactive question, relay session 8d711f67, 2026-11-25.",
  "refs": [
    "S618"
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
