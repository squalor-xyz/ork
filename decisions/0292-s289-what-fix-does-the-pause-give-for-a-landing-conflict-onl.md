---
{
  "id": 292,
  "title": "S289: what fix does the pause give for a landing conflict only in a slicer tracking file no driver merges?",
  "status": "decided",
  "key": "landing.tracking-conflict-fix",
  "asked_by": "claude",
  "decided_by": "Jon",
  "via": "interactive question, relay session 8d711f67, 2026-11-11",
  "created": "2026-11-11",
  "decided_at": "2026-11-11",
  "context": "The 2026-11-04 answer (take base) was reopened 2026-11-07: on U36 the base copy would have dropped the branch's Files edit.",
  "options": [
    {
      "label": "merge-both",
      "summary": "say both sides changed it, give the commands to see each side, keep both changes and remove the markers, then ork retry"
    },
    {
      "label": "take-base",
      "summary": "git checkout --theirs -- FILE"
    },
    {
      "label": "take-branch",
      "summary": "git checkout --ours -- FILE"
    }
  ],
  "recommendation": {
    "label": "merge-both",
    "reasoning": "Only option that loses neither side's edit."
  },
  "decision": "merge-both",
  "rationale": "Owner picked Merge both sides (recommended).",
  "refs": [
    "S289"
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
