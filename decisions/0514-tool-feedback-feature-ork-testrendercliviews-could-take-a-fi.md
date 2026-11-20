---
{
  "id": 514,
  "title": "Tool feedback (feature, ork): TestRenderCLIViews could take a fixture name so a UI pass can render wave start and a stat",
  "status": "decided",
  "key": "tool-feedback.ork-92ad7e77",
  "decided_by": "Jon",
  "created": "2026-11-19",
  "decided_at": "2026-11-19",
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
  "decision": "file",
  "rationale": "Owner chose 'Fold into S499 (Recommended)' in an interactive question, relay session 8d711f67, 2026-11-19: file it as part of S499 (adj 513), not as its own slice.",
  "refs": [
    "S342"
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
