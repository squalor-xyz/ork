---
{
  "id": 470,
  "title": "Which relay contract versions does ork accept once slicer prints v13?",
  "status": "decided",
  "key": "boundary.contract-set",
  "asked_by": "Jon",
  "decided_by": "Jon",
  "created": "2026-11-18",
  "decided_at": "2026-11-18",
  "context": "slicer s460 sets RELAY_CONTRACT = 13. adj 326 accepts v11-v12.",
  "options": [
    {
      "label": "v12-v13",
      "summary": "Accept v12 and v13, role prompts move to v13; retire v11 fixtures (current and one back, adj 175/326)"
    },
    {
      "label": "v11-v13",
      "summary": "Accept v11-v13, prompts move to v13"
    },
    {
      "label": "v11-v13-prompts-v12",
      "summary": "Accept v11-v13, prompts stay on v12"
    }
  ],
  "recommendation": {
    "label": "v12-v13",
    "reasoning": "Current and one back, the rule applied in adj 175 and 326."
  },
  "decision": "v12-v13",
  "rationale": "Owner chose 'v12-v13, prompts v13 (Recommended)' in an interactive question, relay session 8d711f67, 2026-11-18.",
  "supersedes": [
    326
  ],
  "refs": [
    "S432"
  ],
  "history": [
    {
      "date": "2026-11-18",
      "event": "ask"
    },
    {
      "date": "2026-11-18",
      "event": "decide"
    },
    {
      "date": "2026-11-18",
      "event": "supersede",
      "note": "supersedes 326"
    }
  ]
}
---
