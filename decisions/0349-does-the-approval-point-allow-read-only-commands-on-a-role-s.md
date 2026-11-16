---
{
  "id": 349,
  "title": "Does the approval point allow read-only commands on a role's own assigned files?",
  "status": "decided",
  "key": "roles.local-approval-own-files",
  "asked_by": "claude",
  "decided_by": "Jon",
  "recorded_by": "claude",
  "via": "chat, ork relay session 8d711f67",
  "created": "2026-11-14",
  "decided_at": "2026-11-14",
  "options": [
    {
      "label": "allow-readonly",
      "summary": "ls, wc, head, tail, cat, stat on the role's own temp dir, report path and progress note are allowed without asking the owner"
    },
    {
      "label": "ask",
      "summary": "they go to the owner like any uncovered call"
    }
  ],
  "recommendation": {
    "label": "allow-readonly",
    "reasoning": "S304 trial 3: the only two owner questions in the first 22 minutes were ls of the role's own files."
  },
  "decision": "allow-readonly",
  "rationale": "Owner, 2026-11-14, ork relay session 8d711f67, chat: 'Yes' to filing a slice that moves the progress note into the run's tmp dir and lets the hook allow read-only commands on the role's own files (after weighing state dir vs .scratch: .scratch is not excluded in slicer or sluice; docs/state.md:25).",
  "history": [
    {
      "date": "2026-11-14",
      "event": "ask"
    },
    {
      "date": "2026-11-14",
      "event": "decide"
    }
  ]
}
---
