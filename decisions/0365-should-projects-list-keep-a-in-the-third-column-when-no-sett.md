---
{
  "id": 365,
  "title": "Should projects list keep a '-' in the third column when no setting is on?",
  "status": "decided",
  "key": "cli.projects-list-empty-cell",
  "asked_by": "Jon",
  "decided_by": "Jon",
  "created": "2026-11-16",
  "decided_at": "2026-11-16",
  "context": "S241 UI pass question (run ork-20261116-015040-d93b); refines adj 363 (cli.plan-launch-output).",
  "options": [
    {
      "label": "dash",
      "summary": "print '-' for an empty setting cell so the columns stay aligned"
    },
    {
      "label": "two-column",
      "summary": "keep the pinned two-column empty row; the third column can misalign"
    }
  ],
  "recommendation": {
    "label": "dash",
    "reasoning": "the tabwriter column block breaks at a two-column row"
  },
  "decision": "dash",
  "rationale": "Owner chose 'Dash (Recommended)' in an interactive question, relay session 8d711f67, 2026-11-16.",
  "refs": [
    "S241"
  ],
  "history": [
    {
      "date": "2026-11-16",
      "event": "ask"
    },
    {
      "date": "2026-11-16",
      "event": "decide"
    }
  ]
}
---
