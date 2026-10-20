---
{
  "id": 119,
  "title": "What happens when a diff touches files outside the slice's Files list?",
  "status": "decided",
  "key": "engine.files-scope",
  "asked_by": "claude",
  "decided_by": "jon",
  "created": "2026-10-20",
  "decided_at": "2026-10-20",
  "context": "S50 asks for a Files-list check; no slicer CLI prints one item's Files paths yet.",
  "options": [
    {
      "label": "warn",
      "summary": "A warning: record it and add it to the gate brief; paths come from slicer, not from ork's own parser"
    }
  ],
  "recommendation": {
    "label": "warn",
    "reasoning": "Owner answer 2026-10-20 in the roadmap review."
  },
  "decision": "warn",
  "rationale": "Owner 2026-10-20: Files lists are prose and often incomplete, so a stop would give false positives.",
  "refs": [
    "S50"
  ],
  "history": [
    {
      "date": "2026-10-20",
      "event": "ask"
    },
    {
      "date": "2026-10-20",
      "event": "decide"
    }
  ]
}
---
