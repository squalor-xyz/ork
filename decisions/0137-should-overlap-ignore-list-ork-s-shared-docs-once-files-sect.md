---
{
  "id": 137,
  "title": "Should overlap_ignore list ork's shared docs once files_section is set?",
  "status": "decided",
  "key": "tracking.files-section",
  "asked_by": "claude",
  "decided_by": "Jon R",
  "created": "2026-10-20",
  "decided_at": "2026-10-20",
  "context": "plan-parallel needs files_section and reads backticked paths only (slicer/src/slicer/paths.py:23-48; ops.py:2170-2176). Many ork slices touch docs/design.md and docs/dashboard.md, which would keep them in separate waves.",
  "options": [
    {
      "label": "none",
      "summary": "Set files_section Files; no overlap_ignore"
    },
    {
      "label": "docs",
      "summary": "Set files_section Files and overlap_ignore docs/design.md and docs/dashboard.md"
    }
  ],
  "recommendation": {
    "label": "none",
    "reasoning": "A shared doc edit is a real merge conflict; start strict and relax if waves are too narrow."
  },
  "decision": "none",
  "rationale": "Owner accepted the recommendation (2026-10-20).",
  "refs": [
    "S79"
  ],
  "history": [
    {
      "date": "2026-10-20",
      "event": "ask"
    },
    {
      "date": "2026-10-20",
      "event": "decide"
    }
  ]
}
---
