---
{
  "id": 656,
  "title": "Tool feedback (feature, ork): give the gate a separate evidence line for intended base-overlay revert failures so a PASS",
  "status": "decided",
  "key": "tool-feedback.ork-51040795",
  "decided_by": "Jon",
  "created": "2026-11-27",
  "decided_at": "2026-11-27",
  "context": "Proposal: give the gate a separate evidence line for intended base-overlay revert failures so a PASS can count them without the report-shape check refusing it\nRole: gate\nRun: ork-20261127-194939-ea1c\nProject: ork\nItem: S455\nEvidence: attempt 1 was refused with \"PASS reports failed tests\" because the Tests line counted the intended overlay-2 red; open item S649 covers this",
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
  "rationale": "Covered by open slice S649: Jon chose 'Drop, covered by S649' in an interactive question, relay session 8d711f67, 2026-11-27.",
  "refs": [
    "S455"
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
