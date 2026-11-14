---
{
  "id": 348,
  "title": "Which file may a local read-only role write?",
  "status": "decided",
  "key": "roles.local-progress-permission",
  "asked_by": "claude",
  "decided_by": "Jon",
  "recorded_by": "claude",
  "via": "chat, ork relay session 8d711f67",
  "created": "2026-11-14",
  "decided_at": "2026-11-14",
  "context": "Supersedes 343 after the note moved (adj 347).",
  "options": [
    {
      "label": "own-progress",
      "summary": "only its own progress note, now in the run's tmp dir (adj 347)"
    },
    {
      "label": "none",
      "summary": "no file"
    }
  ],
  "recommendation": {
    "label": "own-progress",
    "reasoning": "Same scoped exception as 343, new path."
  },
  "decision": "own-progress",
  "rationale": "Owner, 2026-11-14, ork relay session 8d711f67, chat: 'Yes' to filing a slice that moves the progress note into the run's tmp dir and lets the hook allow read-only commands on the role's own files (after weighing state dir vs .scratch: .scratch is not excluded in slicer or sluice; docs/state.md:25).",
  "supersedes": [
    343
  ],
  "refs": [
    "S358"
  ],
  "history": [
    {
      "date": "2026-11-14",
      "event": "ask"
    },
    {
      "date": "2026-11-14",
      "event": "decide"
    },
    {
      "date": "2026-11-14",
      "event": "supersede",
      "note": "supersedes 343"
    }
  ]
}
---
