---
{
  "id": 528,
  "title": "Tool feedback (feature, ork): give TestRenderCLIViews a status fixture with engine runs, a multi-line wait, findings and",
  "status": "decided",
  "key": "tool-feedback.ork-37b60e4b",
  "decided_by": "Jon",
  "created": "2026-11-20",
  "decided_at": "2026-11-21",
  "context": "Proposal: give TestRenderCLIViews a status fixture with engine runs, a multi-line wait, findings and a relay row, so the status render shows the run rows\nRole: ui\nRun: ork-20261120-152559-6a31\nProject: ork\nItem: S240\nEvidence: the status.txt from `go test ./internal/cli -run TestRenderCLIViews -renders DIR` holds only three project headers and rebuild lines (render_test.go:25 uses statusRebuildFixture)",
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
  "rationale": "Owner chose 'Fold into S499 (Recommended)' in an interactive question, relay session 8d711f67, 2026-11-21: part of S499, not its own slice.",
  "refs": [
    "S240"
  ],
  "history": [
    {
      "date": "2026-11-20",
      "event": "ask"
    },
    {
      "date": "2026-11-21",
      "event": "decide"
    }
  ]
}
---
