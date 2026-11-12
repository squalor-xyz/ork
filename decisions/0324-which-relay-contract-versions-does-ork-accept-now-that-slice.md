---
{
  "id": 324,
  "title": "Which relay contract versions does ork accept, now that slicer 1.21.0 prints relay v12?",
  "status": "decided",
  "key": "boundary.contract-set",
  "asked_by": "claude",
  "decided_by": "Jon",
  "via": "interactive question, relay session 8d711f67, 2026-11-12",
  "created": "2026-11-12",
  "decided_at": "2026-11-12",
  "context": "2026-11-12: slicer 1.21.0 (s453) prints relay v12; ork doctor fails its contract check and new runs refuse to start. v12 adds only advisory tool feedback lines. The slice is S354 (Accept relay v12); the S348 ref was entered before the slice was filed and is wrong (S348 is the merge-driver warning).",
  "options": [
    {
      "label": "v11-v12",
      "summary": "accept v11 and v12, drop v10 (as adj 175 dropped v9 when v11 came); prompts move to relay v12"
    },
    {
      "label": "v10-v12",
      "summary": "accept v10, v11 and v12"
    },
    {
      "label": "v12-only",
      "summary": "accept only v12"
    }
  ],
  "recommendation": {
    "label": "v11-v12",
    "reasoning": "Same rule as adj 175: the current version and one back."
  },
  "decision": "v11-v12",
  "rationale": "Owner picked 'v11 and v12 (Recommended)'; recorded by the agent as the owner's delegate (adj 316).",
  "supersedes": [
    175
  ],
  "refs": [
    "S348"
  ],
  "history": [
    {
      "date": "2026-11-12",
      "event": "ask"
    },
    {
      "date": "2026-11-12",
      "event": "decide"
    },
    {
      "date": "2026-11-12",
      "event": "supersede",
      "note": "supersedes 175"
    },
    {
      "date": "2026-11-12",
      "event": "amend",
      "note": "context",
      "was": {
        "context": "2026-11-12: slicer 1.21.0 (s453) prints relay v12; ork doctor fails its contract check and new runs refuse to start. v12 adds only advisory tool feedback lines."
      }
    }
  ]
}
---
