---
{
  "id": 483,
  "title": "What does the landing's tracking commit do with another item's slice file or render?",
  "status": "decided",
  "key": "landing.tracking-commit-scope",
  "asked_by": "Jon",
  "decided_by": "Jon",
  "created": "2026-11-18",
  "decided_at": "2026-11-18",
  "context": "adj 193 commits every uncommitted tracking edit on main before a landing; slicer f18adb3b held only another item's render.",
  "options": [
    {
      "label": "pause",
      "summary": "Pause and name another item's slice file or render; keep committing the item's own and shared files"
    },
    {
      "label": "keep",
      "summary": "Keep adj 193; retire S351"
    },
    {
      "label": "own",
      "summary": "Commit only the item's own files; pause on the rest"
    }
  ],
  "recommendation": {
    "label": "pause",
    "reasoning": "Recommended in the interactive question."
  },
  "decision": "pause",
  "rationale": "Owner chose 'Pause on others' files (Recommended)' in an interactive question, relay session 8d711f67, 2026-11-18.",
  "narrows": [
    193
  ],
  "refs": [
    "S351"
  ],
  "history": [
    {
      "date": "2026-11-18",
      "event": "ask"
    },
    {
      "date": "2026-11-18",
      "event": "decide"
    },
    {
      "date": "2026-11-18",
      "event": "narrow",
      "note": "narrows 193"
    }
  ]
}
---
