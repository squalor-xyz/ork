---
{
  "id": 558,
  "title": "Tool feedback (feature, ork): add a -render-fixture for roles matrix copy (preview, prompts, stale_preview refusal) so t",
  "status": "decided",
  "key": "tool-feedback.ork-1f3ba451",
  "decided_by": "Jon",
  "created": "2026-11-25",
  "decided_at": "2026-11-25",
  "context": "Proposal: add a -render-fixture for roles matrix copy (preview, prompts, stale_preview refusal) so the UI pass need not write an overlay test\nRole: ui\nRun: ork-20261125-144738-d085\nProject: ork\nItem: S558\nEvidence: TestRenderCLIViews fixtures list has no matrix copy view; I rendered it through an overlay on copyApp",
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
  "rationale": "Owner chose 'Fold into S547' in an interactive question, relay session 8d711f67, 2026-11-25: S547 adds a matrix copy -render-fixture (preview, prompts, stale_preview refusal), not its own slice.",
  "refs": [
    "S558"
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
