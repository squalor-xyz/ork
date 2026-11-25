---
{
  "id": 542,
  "title": "How does a UI pass choose which fixtures TestRenderCLIViews renders?",
  "status": "decided",
  "key": "ui.render-fixture-select",
  "asked_by": "Jon",
  "decided_by": "Jon",
  "created": "2026-11-24",
  "decided_at": "2026-11-24",
  "context": "S499 spec check: the render test writes a fixed set of five files; S499 and folded S534 add fixtures.",
  "options": [
    {
      "label": "fixture-flag",
      "summary": "a test flag -render-fixture NAME[,NAME...] beside -renders; default set unchanged"
    },
    {
      "label": "render-all",
      "summary": "every -renders run writes every fixture"
    }
  ],
  "recommendation": {
    "label": "fixture-flag",
    "reasoning": "Recommended in the interactive question."
  },
  "decision": "fixture-flag",
  "rationale": "Owner chose 'Fixture flag' in an interactive question, relay session 8d711f67, 2026-11-24.",
  "refs": [
    "S499"
  ],
  "history": [
    {
      "date": "2026-11-24",
      "event": "ask"
    },
    {
      "date": "2026-11-24",
      "event": "decide"
    }
  ]
}
---
