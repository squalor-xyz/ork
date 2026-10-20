---
{
  "id": 11,
  "title": "Does ork reimplement slicer's roadmap editor or reuse slicer tui?",
  "status": "decided",
  "key": "ui.slicer-tui",
  "asked_by": "claude",
  "decided_by": "jon",
  "created": "2026-10-18",
  "decided_at": "2026-10-20",
  "context": "slicer has an editing TUI (slicer/docs/tui.md) that calls the same ops as the CLI. ork must edit roadmaps (PROJECT.md goal 2).",
  "options": [
    {
      "label": "cli-editor",
      "summary": "ork's own editor over slicer CLI calls, consistent with the rest of the dashboard"
    },
    {
      "label": "open-slicer-tui",
      "summary": "ork suspends and opens slicer tui --root P for deep edits; ork does only reorder, start and park itself"
    },
    {
      "label": "both",
      "summary": "Common edits in ork, slicer tui on a key for the rest"
    }
  ],
  "recommendation": {
    "label": "both",
    "reasoning": "Keeps the common actions one keystroke away without rebuilding every slicer editor."
  },
  "decision": "both",
  "rationale": "Owner 2026-10-20: agrees with the recommendation. Common edits (reorder, park, start) in ork; one key opens slicer tui for the rest.",
  "history": [
    {
      "date": "2026-10-18",
      "event": "ask"
    },
    {
      "date": "2026-10-20",
      "event": "decide"
    }
  ]
}
---
