---
{
  "id": 297,
  "title": "S328: what does the landing pause say after naming the missing slicer merge drivers?",
  "status": "decided",
  "key": "doctor.merge-driver-pause-text",
  "asked_by": "claude",
  "decided_by": "Jon",
  "via": "interactive question, relay session 8d711f67, 2026-11-11",
  "created": "2026-11-11",
  "decided_at": "2026-11-11",
  "context": "S328 reviewer 2026-11-11: drivers apply only to the next merge; a retry after setup finds the same conflict.",
  "options": [
    {
      "label": "next-time",
      "summary": "drivers apply to the next merge; resolve this conflict by hand and run slicer setup-git and its lines (or ork doctor --fix) before the next landing"
    },
    {
      "label": "abort-hint",
      "summary": "set up, then git merge --abort in the worktree and ork retry"
    },
    {
      "label": "keep",
      "summary": "set up, then ork retry"
    }
  ],
  "recommendation": {
    "label": "next-time",
    "reasoning": "Accurate; keeps hand resolutions."
  },
  "decision": "next-time",
  "rationale": "Owner picked Next time (recommended).",
  "refs": [
    "S328"
  ],
  "history": [
    {
      "date": "2026-11-11",
      "event": "ask"
    },
    {
      "date": "2026-11-11",
      "event": "decide"
    }
  ]
}
---
