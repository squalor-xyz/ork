---
{
  "id": 515,
  "title": "Tool feedback (feature, ork): For CLI views that only a single command's test helper produces, name in the brief the hel",
  "status": "open",
  "key": "tool-feedback.ork-1704c1d8",
  "created": "2026-11-19",
  "context": "Proposal: For CLI views that only a single command's test helper produces, name in the brief the helper to call from an overlay test, as AGENTS.md does for TUI views\nRole: ui\nRun: ork-20261120-025522-97bb\nProject: ork\nItem: S337\nEvidence: TestRenderCLIViews covers only status, show and help, so this view needed a hand-written overlay test calling runWaveNoTerminal",
  "options": [
    {
      "label": "file",
      "summary": "ork files it as a slice in ork"
    },
    {
      "label": "drop",
      "summary": "not acted on"
    }
  ],
  "recommendation": {
    "label": "file",
    "reasoning": "A role proposed it; code cannot judge its cost."
  },
  "refs": [
    "S337"
  ],
  "history": [
    {
      "date": "2026-11-19",
      "event": "ask"
    }
  ]
}
---
