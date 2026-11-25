---
{
  "id": 551,
  "title": "Tool feedback (feature, ork): let the CLI render flag take a named command scenario, so changed CLI views beyond status,",
  "status": "decided",
  "key": "tool-feedback.ork-7b6b90e7",
  "decided_by": "Jon",
  "created": "2026-11-25",
  "decided_at": "2026-11-25",
  "context": "Proposal: let the CLI render flag take a named command scenario, so changed CLI views beyond status, show and help render without an overlay test\nRole: ui\nRun: ork-20261125-131825-4682\nProject: ork\nItem: S548\nEvidence: TestRenderCLIViews renders only status.txt, show.txt and help.txt; S548 changes only the roles matrix copy output, so it needed a hand-written overlay test.",
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
  "decision": "drop",
  "rationale": "Owner chose 'Drop: S499 covers it' in an interactive question, relay session 8d711f67, 2026-11-25: S499's -render-fixture and fake-reply fixture cover named command views, and S552 tells the UI pass about them.",
  "refs": [
    "S548"
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
