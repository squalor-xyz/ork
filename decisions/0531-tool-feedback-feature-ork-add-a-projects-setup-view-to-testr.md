---
{
  "id": 531,
  "title": "Tool feedback (feature, ork): add a projects-setup view to TestRenderCLIViews so the UI pass can render new CLI subcomma",
  "status": "decided",
  "key": "tool-feedback.ork-8efffc86",
  "decided_by": "Jon",
  "created": "2026-11-21",
  "decided_at": "2026-11-24",
  "context": "Proposal: add a projects-setup view to TestRenderCLIViews so the UI pass can render new CLI subcommands without an overlay test\nRole: ui\nRun: ork-20261122-035954-42fc\nProject: ork\nItem: S249\nEvidence: internal/cli/render_test.go:27-41 renders only status, status-summary, show and help, so I needed an overlay test for the new setup output",
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
  "rationale": "Owner chose 'File one slice, fold 531' in an interactive question, relay session 8d711f67, 2026-11-25: fold it into the slice filed for adj 532, not as its own slice.",
  "refs": [
    "S249"
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
