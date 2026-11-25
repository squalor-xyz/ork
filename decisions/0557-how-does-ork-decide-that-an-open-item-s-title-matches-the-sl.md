---
{
  "id": 557,
  "title": "How does ork decide that an open item's title matches the slice's Why text?",
  "status": "decided",
  "key": "brief.direction-duplicate-match",
  "asked_by": "Jon",
  "decided_by": "Jon",
  "created": "2026-11-25",
  "decided_at": "2026-11-25",
  "context": "S557 spec check: titleWords in internal/engine/neardup.go; S552's Why vs S550's title is the motivating pair.",
  "options": [
    {
      "label": "containment",
      "summary": "flag an open item when at least 0.6 of its title's content words appear in the Why"
    },
    {
      "label": "title-title",
      "summary": "NearDuplicateTitles at 0.5 on titles"
    },
    {
      "label": "exact-ids",
      "summary": "only exact id or title in the Why"
    }
  ],
  "recommendation": {
    "label": "containment",
    "reasoning": "Recommended in the interactive question."
  },
  "decision": "containment",
  "rationale": "Owner chose 'Word share' in an interactive question, relay session 8d711f67, 2026-11-25.",
  "refs": [
    "S557"
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
