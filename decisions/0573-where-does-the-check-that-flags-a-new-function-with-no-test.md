---
{
  "id": 573,
  "title": "Where does the check that flags a new function with no test route to its product call sites live, and how is it built?",
  "status": "decided",
  "key": "review.untested-call-site-check",
  "asked_by": "Jon",
  "decided_by": "Jon",
  "created": "2026-11-25",
  "decided_at": "2026-11-25",
  "context": "S564 spec check.",
  "options": [
    {
      "label": "ork-code-warn",
      "summary": "deterministic go/parser + go/ast step on the slice diff; lists unreached new functions in the reviewer brief as advisory, never blocks"
    },
    {
      "label": "reviewer-prompt",
      "summary": "a line in the reviewer prompt"
    },
    {
      "label": "slicer-feature",
      "summary": "move to slicer"
    },
    {
      "label": "retire",
      "summary": "retire S564"
    }
  ],
  "recommendation": {
    "label": "ork-code-warn",
    "reasoning": "Recommended in the interactive question."
  },
  "decision": "ork-code-warn",
  "rationale": "Owner chose 'ork code, warn in brief' in an interactive question, relay session 8d711f67, 2026-11-25.",
  "refs": [
    "S564"
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
