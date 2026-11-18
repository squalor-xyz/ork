---
{
  "id": 469,
  "title": "Does the landed action line keep the rebuild hint for a project with auto_rebuild on?",
  "status": "decided",
  "key": "ui.landed-rebuild-hint",
  "asked_by": "Jon",
  "decided_by": "Jon",
  "created": "2026-11-18",
  "decided_at": "2026-11-18",
  "context": "S431 implementer (2026-11-18): the TUI's landed line offers ork daemon restart --rebuild (adj 232, superseded by 464); with auto_rebuild on the daemon rebuilds itself.",
  "options": [
    {
      "label": "hide-now",
      "summary": "drop the hint for opted-in projects in S431"
    },
    {
      "label": "later",
      "summary": "keep it and file a follow-up slice"
    }
  ],
  "recommendation": {
    "label": "hide-now",
    "reasoning": "The hint asks for the hand command the slice removes, and that command refuses while the drain's roles run."
  },
  "decision": "hide-now",
  "rationale": "Owner chose 'Hide it in S431 (Recommended)' in an interactive question, relay session 8d711f67, 2026-11-18.",
  "refs": [
    "S431"
  ],
  "history": [
    {
      "date": "2026-11-18",
      "event": "ask"
    },
    {
      "date": "2026-11-18",
      "event": "decide"
    }
  ]
}
---
