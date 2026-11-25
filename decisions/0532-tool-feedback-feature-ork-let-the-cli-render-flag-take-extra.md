---
{
  "id": 532,
  "title": "Tool feedback (feature, ork): let the CLI render flag take extra views such as a fake-reply command, so a new subcommand",
  "status": "decided",
  "key": "tool-feedback.ork-9c04f088",
  "decided_by": "Jon",
  "created": "2026-11-21",
  "decided_at": "2026-11-24",
  "context": "Proposal: let the CLI render flag take extra views such as a fake-reply command, so a new subcommand's text can be rendered without an overlay test\nRole: ui\nRun: ork-20261122-041409-eedc\nProject: ork\nItem: S250\nEvidence: TestRenderCLIViews writes only status, show and help; the apply views needed render_overlay_test.go through -overlay",
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
  "rationale": "Owner chose 'File one slice, fold 531' in an interactive question, relay session 8d711f67, 2026-11-25: file it as one slice that lets the CLI render flag take extra views.",
  "refs": [
    "S250"
  ],
  "history": [
    {
      "date": "2026-11-21",
      "event": "ask"
    },
    {
      "date": "2026-11-24",
      "event": "decide"
    }
  ]
}
---
