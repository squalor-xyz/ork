---
{
  "id": 175,
  "title": "Which relay contract versions does ork accept?",
  "status": "decided",
  "key": "boundary.contract-set",
  "asked_by": "claude",
  "decided_by": "jon",
  "created": "2026-10-30",
  "decided_at": "2026-10-30",
  "context": "Record 67 (boundary.contract) decided that ork keeps an accepted set; this record names its contents after the v11 bump. Slice: S198.",
  "options": [
    {
      "label": "10-and-11",
      "summary": "Accept relay v10 and v11; drop v9"
    },
    {
      "label": "9-10-and-11",
      "summary": "Keep v9 beside v10 and v11"
    }
  ],
  "recommendation": {
    "label": "10-and-11",
    "reasoning": "slicer 1.17.0 serves only v11 text; v9 predates the contract data ork now reads; fewer versions to carry. Owner already stated this in S198 on 2026-10-30."
  },
  "decision": "10-and-11",
  "rationale": "Owner picked this option in an interactive question in the ork v1.0 session on 2026-10-30; recorded by the agent at the owner's direction (process.owner-answers, 170).",
  "refs": [
    "S198",
    "S142"
  ],
  "history": [
    {
      "date": "2026-10-30",
      "event": "ask"
    },
    {
      "date": "2026-10-30",
      "event": "decide"
    }
  ]
}
---
