---
{
  "id": 394,
  "title": "Which cloud role carves the slivers for a local implementer, and how does its result become sliver items?",
  "status": "decided",
  "key": "roles.local-sliver-carver-role",
  "asked_by": "Jon",
  "decided_by": "Jon",
  "created": "2026-11-16",
  "decided_at": "2026-11-16",
  "context": "S359 spec check question.",
  "options": [
    {
      "label": "spec-check-row",
      "summary": "reuse the spec check row with a carve prompt; read-only; its report lists the slivers (title, files, tests, checks); ork files each with slicer add --parent PARENT --size S --effort 1 via a Parent field on AddSpec"
    },
    {
      "label": "new-role",
      "summary": "a new read-only carver role with its own matrix row and prompt"
    },
    {
      "label": "reuse-other",
      "summary": "another role the owner names"
    }
  ],
  "recommendation": {
    "label": "spec-check-row",
    "reasoning": "the only cloud read-only role the spec names, and ork already runs it; adj 337 fixes who carves but not which role"
  },
  "decision": "spec-check-row",
  "rationale": "Owner chose 'Spec-check row (Recommended)' in an interactive question, relay session 8d711f67, 2026-11-16.",
  "refs": [
    "S359"
  ],
  "history": [
    {
      "date": "2026-11-16",
      "event": "ask"
    },
    {
      "date": "2026-11-16",
      "event": "decide"
    }
  ]
}
---
