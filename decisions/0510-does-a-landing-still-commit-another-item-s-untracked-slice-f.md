---
{
  "id": 510,
  "title": "Does a landing still commit another item's untracked slice files that the index lists?",
  "status": "decided",
  "key": "landing.index-listed-files",
  "asked_by": "Jon",
  "decided_by": "Jon",
  "created": "2026-11-19",
  "decided_at": "2026-11-19",
  "context": "adj 483 pauses on another item's slice file or render; S454 commits the untracked slice files the index lists (run ork-20261119-232111-8171).",
  "options": [
    {
      "label": "keep",
      "summary": "Keep S454: commit the listed untracked slice files with the index; the adj 483 pause applies to other changed slice files"
    },
    {
      "label": "pause",
      "summary": "Pause and name them"
    },
    {
      "label": "index-out",
      "summary": "Leave the index uncommitted"
    }
  ],
  "recommendation": {
    "label": "keep",
    "reasoning": "Recommended in the interactive question."
  },
  "decision": "keep",
  "rationale": "Owner chose 'Keep committing them (Recommended)' in an interactive question, relay session 8d711f67, 2026-11-19.",
  "refs": [
    "S351"
  ],
  "history": [
    {
      "date": "2026-11-19",
      "event": "ask"
    },
    {
      "date": "2026-11-19",
      "event": "decide"
    }
  ]
}
---
