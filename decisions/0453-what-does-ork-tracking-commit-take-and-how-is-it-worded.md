---
{
  "id": 453,
  "title": "What does ork tracking commit take and how is it worded?",
  "status": "decided",
  "key": "cli.tracking-commit",
  "asked_by": "Jon",
  "decided_by": "Jon",
  "created": "2026-11-17",
  "decided_at": "2026-11-17",
  "context": "Automation slice question (owner rule: deterministic work in code), 2026-11-17.",
  "options": [
    {
      "label": "slicer-decisions-auto",
      "summary": ".slicer/ minus config.json and relay-roles.json, plus decisions/; message built from paths; refuses other staged paths; --leave PATH"
    },
    {
      "label": "message-required",
      "summary": "caller passes -m"
    }
  ],
  "recommendation": {
    "label": "slicer-decisions-auto",
    "reasoning": "drafter's recommendation, automation slices"
  },
  "decision": "slicer-decisions-auto",
  "rationale": "Owner chose '.slicer + decisions/, auto msg (Rec)' in an interactive question, relay session 8d711f67, 2026-11-17.",
  "history": [
    {
      "date": "2026-11-17",
      "event": "ask"
    },
    {
      "date": "2026-11-17",
      "event": "decide"
    }
  ]
}
---
