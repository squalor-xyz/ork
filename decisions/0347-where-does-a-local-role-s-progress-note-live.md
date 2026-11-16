---
{
  "id": 347,
  "title": "Where does a local role's progress note live?",
  "status": "decided",
  "key": "roles.local-progress-file",
  "asked_by": "claude",
  "decided_by": "Jon",
  "recorded_by": "claude",
  "via": "chat, ork relay session 8d711f67",
  "created": "2026-11-14",
  "decided_at": "2026-11-14",
  "context": "Supersedes 336 (scratch-note) and the path half of 343.",
  "options": [
    {
      "label": "run-tmp",
      "summary": "in the run's state dir, ~/.local/state/ork/runs/RUN/tmp/progress-ROLE.md, carried into the fresh session's brief"
    },
    {
      "label": "scratch-note",
      "summary": ".scratch/progress-RUN-ROLE.md in the main checkout"
    }
  ],
  "recommendation": {
    "label": "run-tmp",
    "reasoning": ".scratch is this repo's convention only; slicer and sluice do not exclude it, and docs/state.md:25 keeps ork's files out of projects."
  },
  "decision": "run-tmp",
  "rationale": "Owner, 2026-11-14, ork relay session 8d711f67, chat: 'Yes' to filing a slice that moves the progress note into the run's tmp dir and lets the hook allow read-only commands on the role's own files (after weighing state dir vs .scratch: .scratch is not excluded in slicer or sluice; docs/state.md:25).",
  "supersedes": [
    336
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
      "note": "supersedes 336"
    }
  ]
}
---
