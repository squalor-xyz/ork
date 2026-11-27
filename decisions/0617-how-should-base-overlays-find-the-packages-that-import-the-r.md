---
{
  "id": 617,
  "title": "How should Base overlays find the packages that import the reverted file's package?",
  "status": "decided",
  "key": "base-overlays.importer-packages",
  "asked_by": "Jon",
  "decided_by": "Jon",
  "created": "2026-11-27",
  "decided_at": "2026-11-27",
  "context": "S620 spec check.",
  "options": [
    {
      "label": "direct-go-list",
      "summary": "go list the module; one vetted overlay command per direct importer, test imports included"
    },
    {
      "label": "transitive",
      "summary": "all dependents, own cap"
    },
    {
      "label": "capped-unvetted",
      "summary": "capped, no vet"
    }
  ],
  "recommendation": {
    "label": "direct-go-list",
    "reasoning": "Recommended in the interactive question."
  },
  "decision": "direct-go-list",
  "rationale": "Owner chose 'Direct importers via go list' in an interactive question, relay session 8d711f67, 2026-11-27.",
  "refs": [
    "S620"
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
