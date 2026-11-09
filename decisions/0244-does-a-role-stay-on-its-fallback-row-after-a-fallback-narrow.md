---
{
  "id": 244,
  "title": "Does a role stay on its fallback row after a fallback (narrowed)?",
  "status": "decided",
  "key": "vendor.fallback-sticky-narrowed",
  "decided_by": "Jon",
  "via": "owner instruction in relay session 8d711f67, 2026-11-09: \"you do those for me\" (owner decisions of 2026-11-07 recorded in S255 and S283 notes)",
  "created": "2026-11-09",
  "decided_at": "2026-11-09",
  "context": "Narrows adj 115. Owner decided the narrowing on 2026-11-07; recorded in the S255 note.",
  "options": [
    {
      "label": "narrowed-sticky",
      "summary": "inside a round (fix rounds, an error or lost pause rerun) the role stays on its fallback row; a fresh-round retry restarts the vendor chain at the first entry when that vendor is back"
    }
  ],
  "recommendation": {
    "label": "narrowed-sticky",
    "reasoning": "Owner decision of 2026-11-07 (adj 115 narrowing)."
  },
  "decision": "narrowed-sticky",
  "rationale": "Owner asked on 2026-11-09 for the supersede of adj 115 to be done, and decided the narrowing on 2026-11-07.",
  "supersedes": [
    115
  ],
  "refs": [
    "S255"
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
      "note": "supersedes 115"
    }
  ]
}
---
