---
{
  "id": 514,
  "title": "Tool feedback (feature, ork): TestRenderCLIViews could take a fixture name so a UI pass can render wave start and a stat",
  "status": "open",
  "key": "tool-feedback.ork-92ad7e77",
  "created": "2026-11-19",
  "context": "Proposal: TestRenderCLIViews could take a fixture name so a UI pass can render wave start and a status with queued or held runs without an overlay test\nRole: ui\nRun: ork-20261120-015050-c27e\nProject: ork\nItem: S342\nEvidence: the -renders fixtures are status (rebuild only), show and help; to render the S342 views I had to add an overlay test file in TMPDIR",
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
    "S342"
  ],
  "history": [
    {
      "date": "2026-11-19",
      "event": "ask"
    }
  ]
}
---
