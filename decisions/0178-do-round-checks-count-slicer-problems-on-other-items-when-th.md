---
{
  "id": 178,
  "title": "Do round checks count slicer problems on other items when the branch is behind its base?",
  "status": "decided",
  "key": "checks.outside-problems",
  "asked_by": "Claude",
  "decided_by": "Jon",
  "created": "2026-11-06",
  "decided_at": "2026-11-06",
  "context": "S279. slicer s313 burned 8 fix rounds on a slicer check problem on s443 that only main's code fixed (2026-11-04).",
  "options": [
    {
      "label": "defer-to-land",
      "summary": "When every slicer problem names another item and the branch is behind its base, the round is not red; problems are logged and shown to reviewer and gate; the landing merge and re-check decide"
    },
    {
      "label": "merge-mid-run",
      "summary": "Merge main into the worktree before the next round"
    },
    {
      "label": "pause",
      "summary": "Pause with the rounds pause and name the problem"
    }
  ],
  "recommendation": {
    "label": "defer-to-land",
    "reasoning": "No git writes mid-run and no wasted rounds; landing already merges and re-checks."
  },
  "decision": "defer-to-land",
  "rationale": "Owner 2026-11-05, interactive question in the ork session (S279 design): Defer to land.",
  "refs": [
    "S279"
  ],
  "history": [
    {
      "date": "2026-11-06",
      "event": "ask"
    },
    {
      "date": "2026-11-06",
      "event": "decide"
    }
  ]
}
---
