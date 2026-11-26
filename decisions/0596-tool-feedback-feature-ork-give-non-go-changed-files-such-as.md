---
{
  "id": 596,
  "title": "Tool feedback (feature, ork): give non-Go changed files such as role prompts a base-revert command in the Base overlays ",
  "status": "decided",
  "key": "tool-feedback.ork-0dd834cb",
  "decided_by": "Jon",
  "created": "2026-11-25",
  "decided_at": "2026-11-25",
  "context": "Proposal: give non-Go changed files such as role prompts a base-revert command in the Base overlays section, or say that none applies\nRole: reviewer\nRun: ork-20261125-224829-7bd0\nProject: ork\nItem: S589\nEvidence: reviewer.md changed in S589 but Base overlays covered only round.go and gomodimports.go, so the roles test revert could only be traced",
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
  "rationale": "Folded into S592: Jon chose 'Fold into S592' in an interactive question, relay session 8d711f67, 2026-11-26.",
  "refs": [
    "S589"
  ],
  "history": [
    {
      "date": "2026-11-25",
      "event": "ask"
    },
    {
      "date": "2026-11-25",
      "event": "decide"
    }
  ]
}
---
