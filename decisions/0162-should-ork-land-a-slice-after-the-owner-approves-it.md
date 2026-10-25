---
{
  "id": 162,
  "title": "Should ork land a slice after the owner approves it?",
  "status": "open",
  "key": "landing.in-ork",
  "asked_by": "claude",
  "created": "2026-10-25",
  "context": "Retrospective R5 (docs/review-2026-10-25-relay-retrospective.md). landing.v1 (#49) keeps landing out of ork: a ready-to-land list and merge detection only, and docs/boundaries.md:17 says ork never merges or lands. The owner wants to develop entirely through ork; in the v1 run, landing was a hand-run script (verify, commit, merge main in, end the relay run, ff-merge, push, remove worktree and branch) that worked on all 28 slices. The owner directed on 2026-10-25 to follow the retrospective's recommendations.",
  "options": [
    {
      "label": "land-after-approve",
      "summary": "after approval ork runs the finish step, commits, merges main in if it moved, re-renders and re-checks, runs slicer land, ff-merges into the base branch, optionally pushes (per-project setting, off by default) and removes the worktree and branch",
      "tradeoffs": "ork writes to the base branch and may push; needs a supersede of #49 and a boundaries.md change; removes the last hand step"
    },
    {
      "label": "keep-list",
      "summary": "keep landing.v1; ork lists ready runs and the owner lands by hand",
      "tradeoffs": "no new git writes in ork; the owner leaves ork to land every slice"
    }
  ],
  "recommendation": {
    "label": "land-after-approve",
    "reasoning": "Landing is deterministic, was reliable as a script, and is the main hand step left between approve and the next wave. slicer land stays the tracking step; ork only sequences it."
  },
  "refs": [
    "landing.v1",
    "docs/boundaries.md:17"
  ],
  "history": [
    {
      "date": "2026-10-25",
      "event": "ask"
    }
  ]
}
---
