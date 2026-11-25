---
{
  "id": 549,
  "title": "How does the settings pane keep the block heading visible when scrolled?",
  "status": "decided",
  "key": "ui.settings-pane-scroll-heading",
  "asked_by": "Jon",
  "decided_by": "Jon",
  "created": "2026-11-25",
  "decided_at": "2026-11-25",
  "context": "S546 spec check: scrolled to the end at 80x24 the slicer config heading leaves the window.",
  "options": [
    {
      "label": "marker",
      "summary": "fold the heading into the +N lines above marker, e.g. +12 lines above (slicer config)"
    },
    {
      "label": "pin",
      "summary": "first body line shows the block heading; body one line shorter"
    },
    {
      "label": "none",
      "summary": "leave as is"
    }
  ],
  "recommendation": {
    "label": "marker",
    "reasoning": "Recommended in the interactive question."
  },
  "decision": "marker",
  "rationale": "Owner chose 'In the marker' in an interactive question, relay session 8d711f67, 2026-11-25.",
  "refs": [
    "S546"
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
