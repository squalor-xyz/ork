---
{
  "id": 81,
  "title": "In what order do approval, the finish step and the commit run?",
  "status": "decided",
  "key": "engine.finish-order",
  "asked_by": "claude",
  "decided_by": "jon",
  "created": "2026-10-19",
  "decided_at": "2026-10-19",
  "context": "S04 commits on approve, then runs the finish step; the relay contract (slicer ai relay-orchestrator, Order steps 3 and 4) runs the finish step before the commit.",
  "options": [
    {
      "label": "finish-then-commit",
      "summary": "on approve, the finish step (closing note, slicer done or handoff), then one commit with code and tracking; a failed commit stops the slice in needs-you with the git error"
    },
    {
      "label": "commit-then-finish",
      "summary": "on approve, commit, then the finish step, with the tracking change committed separately"
    }
  ],
  "decision": "finish-then-commit",
  "rationale": "Owner 2026-10-19: follow the contract; one commit carries code and tracking, a reject leaves tracking untouched, and a failed commit stops for the owner without undoing the finish step.",
  "refs": [
    "S04",
    "S17"
  ],
  "history": [
    {
      "date": "2026-10-19",
      "event": "ask"
    },
    {
      "date": "2026-10-19",
      "event": "decide"
    }
  ]
}
---
