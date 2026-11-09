---
{
  "id": 245,
  "title": "If one item fails to plan, does ork wave start abort (narrowed for a missing spec)?",
  "status": "decided",
  "key": "wave.plan-failure-missing-spec",
  "decided_by": "Jon",
  "via": "owner instruction in relay session 8d711f67, 2026-11-09: \"you do those for me\" (owner decisions of 2026-11-07 recorded in S255 and S283 notes)",
  "created": "2026-11-09",
  "decided_at": "2026-11-09",
  "context": "Narrows adj 146. Owner decided the narrowing on 2026-11-07; recorded in the S283 note.",
  "options": [
    {
      "label": "abort-except-no-spec",
      "summary": "a plan failure aborts before anything starts, except a missing spec: wave start starts the items that have specs and lists the rest with a slicer promote hint"
    }
  ],
  "recommendation": {
    "label": "abort-except-no-spec",
    "reasoning": "Owner decision of 2026-11-07 (adj 146 narrowing)."
  },
  "decision": "abort-except-no-spec",
  "rationale": "Owner asked on 2026-11-09 for the supersede of adj 146 to be done, and decided the narrowing on 2026-11-07.",
  "supersedes": [
    146
  ],
  "refs": [
    "S283"
  ],
  "history": [
    {
      "date": "2026-11-09",
      "event": "ask"
    },
    {
      "date": "2026-11-09",
      "event": "decide"
    },
    {
      "date": "2026-11-09",
      "event": "supersede",
      "note": "supersedes 146"
    }
  ]
}
---
