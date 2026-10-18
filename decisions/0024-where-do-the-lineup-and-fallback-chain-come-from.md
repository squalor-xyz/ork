---
{
  "id": 24,
  "title": "Where do the lineup and fallback chain come from?",
  "status": "decided",
  "key": "lineup.source",
  "asked_by": "claude",
  "decided_by": "jon",
  "created": "2026-10-18",
  "decided_at": "2026-10-18",
  "context": "slicer has a role matrix; agents repo has relay.conf.",
  "options": [
    {
      "label": "matrix-alternatives",
      "summary": "slicer role matrix, chain from its alternatives"
    },
    {
      "label": "ork-config",
      "summary": "ork's own config"
    },
    {
      "label": "mapping",
      "summary": "Fixed cross-vendor mapping"
    }
  ],
  "decision": "matrix-alternatives",
  "rationale": "Owner interview 2026-10-18: slicer relay-roles recommend is the source; the chain is the primary row plus ordered alternatives, editable at launch; relay.conf retires.",
  "history": [
    {
      "date": "2026-10-18",
      "event": "ask"
    },
    {
      "date": "2026-10-18",
      "event": "decide"
    }
  ]
}
---
