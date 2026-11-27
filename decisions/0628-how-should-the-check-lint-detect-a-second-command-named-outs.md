---
{
  "id": 628,
  "title": "How should the Check lint detect a second command named outside the backticks?",
  "status": "decided",
  "key": "spec.check-second-command",
  "asked_by": "Jon",
  "decided_by": "Jon",
  "created": "2026-11-27",
  "decided_at": "2026-11-27",
  "context": "S636 spec check.",
  "options": [
    {
      "label": "known-commands",
      "summary": "flag a clause-leading word from a fixed list (go, gofmt, git, slicer, adj, ork) followed by an argument outside the backticks; the finding names it"
    },
    {
      "label": "path-lookup",
      "summary": "any clause-leading word on PATH"
    },
    {
      "label": "output-claim-only",
      "summary": "flag output claims only"
    }
  ],
  "recommendation": {
    "label": "known-commands",
    "reasoning": "Recommended in the interactive question."
  },
  "decision": "known-commands",
  "rationale": "Owner chose 'Known commands' in an interactive question, relay session 8d711f67, 2026-11-27.",
  "refs": [
    "S636"
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
