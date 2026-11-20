---
{
  "id": 513,
  "title": "Tool feedback (feature, ork): let TestRenderCLIViews render the projects set summary and run error texts from fixtures, ",
  "status": "decided",
  "key": "tool-feedback.ork-3cba84b8",
  "decided_by": "Jon",
  "created": "2026-11-19",
  "decided_at": "2026-11-19",
  "context": "Proposal: let TestRenderCLIViews render the projects set summary and run error texts from fixtures, so a UI pass can put them on the approval card\nRole: ui\nRun: ork-20261120-014727-adb2\nProject: ork\nItem: S307\nEvidence: TestRenderCLIViews writes only status.txt, show.txt and help.txt; this slice changes only projects set and refusal texts",
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
  "decision": "file",
  "rationale": "Owner chose 'File it (Recommended)' in an interactive question, relay session 8d711f67, 2026-11-19.",
  "refs": [
    "S307"
  ],
  "history": [
    {
      "date": "2026-11-19",
      "event": "ask"
    },
    {
      "date": "2026-11-19",
      "event": "decide"
    }
  ]
}
---
