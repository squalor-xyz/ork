---
{
  "id": 193,
  "title": "May ork commit uncommitted tracking edits on main before a landing merges the slice branch?",
  "status": "decided",
  "key": "landing.tracking-commit",
  "asked_by": "claude",
  "decided_by": "Jon",
  "via": "interactive question, relay session 8d711f67, 2026-11-08",
  "created": "2026-11-08",
  "decided_at": "2026-11-08",
  "context": "S254: a landing aborts when the main checkout has uncommitted edits to files the slice branch also changes (the 2026-11-04 prism-ui break class). docs/boundaries.md forbids ork to open, search, parse or edit .slicer/ files; it already lists ork's commits on main (the 'ID done' commit). The owner chose option commit-tracking in the S254 interviews (2026-11-03 and 2026-11-07). The commit takes only the named tracking paths (.slicer/ except config.json and relay-roles.json) that the slice also changes; any other overlapping dirty file stops the landing at once with a one-line pause; ork never stashes, checks out or resets main.",
  "options": [
    {
      "label": "commit-tracking",
      "summary": "ork commits the named tracking files on main (message 'Tracking edits on main before landing ID'), only when the slice also changes them, then merges; other overlapping dirty files stop the landing"
    },
    {
      "label": "stop",
      "summary": "stop at once like any other overlapping file; the owner commits by hand and runs ork retry"
    },
    {
      "label": "stash",
      "summary": "stash and pop (rejected: risks the owner's edits)"
    }
  ],
  "recommendation": {
    "label": "commit-tracking",
    "reasoning": "The owner chose it; a commit by named path loses nothing and removes one hand step on every landing after any filing on main."
  },
  "decision": "commit-tracking",
  "rationale": "Owner picked 'commit-tracking' in the interactive question; Q3 'as built' (only overlap with the slice's changes matters).",
  "refs": [
    "S254"
  ],
  "history": [
    {
      "date": "2026-11-08",
      "event": "ask"
    },
    {
      "date": "2026-11-08",
      "event": "decide"
    }
  ]
}
---
