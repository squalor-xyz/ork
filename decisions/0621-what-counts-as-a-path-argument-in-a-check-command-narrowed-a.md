---
{
  "id": 621,
  "title": "What counts as a path argument in a Check command (narrowed), and what does a missing one do before round 1?",
  "status": "decided",
  "key": "checks.path-arguments-narrow",
  "asked_by": "Jon",
  "decided_by": "Jon",
  "created": "2026-11-27",
  "decided_at": "2026-11-27",
  "context": "S615 reviewer: -run subtest patterns, Go import paths and rev:path words were read as paths and refused valid Checks. Narrows adj 614.",
  "options": [
    {
      "label": "narrow-now",
      "summary": "as adj 614 (relative word with / or leading ./, missing one refuses the start, Files-created paths and their directories exempt), but the value of a -run, -bench or -skip flag, a rev:path word and a Go import path are not path arguments"
    },
    {
      "label": "keep",
      "summary": "adj 614 as decided"
    },
    {
      "label": "narrow-later",
      "summary": "follow-up slice"
    }
  ],
  "recommendation": {
    "label": "narrow-now",
    "reasoning": "Recommended in the interactive question."
  },
  "decision": "narrow-now",
  "rationale": "Owner chose 'Narrow now' in an interactive question, relay session 8d711f67, 2026-11-27.",
  "supersedes": [
    614
  ],
  "refs": [
    "S615"
  ],
  "history": [
    {
      "date": "2026-11-27",
      "event": "ask"
    },
    {
      "date": "2026-11-27",
      "event": "decide"
    },
    {
      "date": "2026-11-27",
      "event": "supersede",
      "note": "supersedes 614"
    }
  ]
}
---
