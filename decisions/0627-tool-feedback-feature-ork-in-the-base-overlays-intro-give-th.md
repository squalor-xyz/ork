---
{
  "id": 627,
  "title": "Tool feedback (feature, ork): In the Base overlays intro, give the -count=1 form so a reviewer's revert run is never ser",
  "status": "decided",
  "key": "tool-feedback.ork-c7095873",
  "decided_by": "Jon",
  "created": "2026-11-27",
  "decided_at": "2026-11-27",
  "context": "Proposal: In the Base overlays intro, give the -count=1 form so a reviewer's revert run is never served from the test cache\nRole: reviewer\nRun: ork-20261127-141336-9ee9\nProject: ork\nItem: S622\nEvidence: I added -count=1 by hand to the overlay-1.json run to be sure the red result was fresh.",
  "options": [
    {
      "label": "file",
      "summary": "ork files it as a slice in ork"
    },
    {
      "label": "drop",
      "summary": "not acted on"
    }
  ],
  "recommendation": {
    "label": "file",
    "reasoning": "A role proposed it; code cannot judge its cost."
  },
  "decision": "drop",
  "rationale": "Folded into S620's spec: Jon chose 'Add to S620's spec' in an interactive question, relay session 8d711f67, 2026-11-27.",
  "refs": [
    "S622"
  ],
  "history": [
    {
      "date": "2026-11-27",
      "event": "ask"
    },
    {
      "date": "2026-11-27",
      "event": "decide"
    }
  ]
}
---
