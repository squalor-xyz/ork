---
{
  "id": 343,
  "title": "S358: how do local read-only roles keep the .scratch progress note?",
  "status": "decided",
  "key": "roles.local-progress-permission",
  "asked_by": "claude",
  "decided_by": "Jon",
  "recorded_by": "claude",
  "via": "interactive question, ork relay session 8d711f67",
  "created": "2026-11-13",
  "decided_at": "2026-11-13",
  "context": "Implementer question in run ork-20261112-123335-e040: read-only roles cannot write the note adj 336 puts in .scratch/.",
  "options": [
    {
      "label": "scoped-write",
      "summary": "permit writing only the role's own progress file in the main checkout's .scratch/, with matching permission and brief exceptions"
    },
    {
      "label": "engine-saves",
      "summary": "roles stay read-only, print progress in their output, and ork saves it to the scratch file"
    }
  ],
  "recommendation": {
    "label": "scoped-write",
    "reasoning": "Keeps the role-written note (adj 336) and limits the permission change to one file."
  },
  "decision": "scoped-write",
  "rationale": "Owner picked scoped-write in an interactive question on 2026-11-13 (ork relay session 8d711f67), answering S358 run ork-20261112-123335-e040; recorded as the owner's delegate (process.owner-answers, 316).",
  "refs": [
    "S358"
  ],
  "history": [
    {
      "date": "2026-11-13",
      "event": "ask"
    },
    {
      "date": "2026-11-13",
      "event": "decide"
    }
  ]
}
---
