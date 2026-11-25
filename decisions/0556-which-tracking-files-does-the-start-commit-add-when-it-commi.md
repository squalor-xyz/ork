---
{
  "id": 556,
  "title": "Which tracking files does the start commit add when it commits .slicer/index.json and the index change comes from a retire move?",
  "status": "decided",
  "key": "run.start-commit-tracking-moves",
  "asked_by": "Jon",
  "decided_by": "Jon",
  "created": "2026-11-25",
  "decided_at": "2026-11-25",
  "context": "S543 spec check: commitLaunchTracking and listedUntrackedSlices (internal/engine/landdirty.go:125-189) carry only the index, the item's own files and listed slices.",
  "options": [
    {
      "label": "moves-renders",
      "summary": "also commit each slice-file move and its render; leave other dirty tracking files alone"
    },
    {
      "label": "all-tracking",
      "summary": "commit every changed .slicer/ file except config.json and relay-roles.json"
    },
    {
      "label": "pause",
      "summary": "pause the run when moves are pending"
    }
  ],
  "recommendation": {
    "label": "moves-renders",
    "reasoning": "Recommended in the interactive question."
  },
  "decision": "moves-renders",
  "rationale": "Owner chose 'Moves and renders' in an interactive question, relay session 8d711f67, 2026-11-25.",
  "refs": [
    "S543"
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
