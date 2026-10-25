---
{
  "id": 160,
  "title": "Do [ and ] switch projects, freeing left and right for in-view tabs and fields?",
  "status": "decided",
  "key": "ui.project-keys",
  "decided_by": "Jon R",
  "created": "2026-10-25",
  "decided_at": "2026-10-25",
  "context": "Design review 2026-10-25: left/right are project keys everywhere (adj 120, 112), tabs on the Decisions view (S59), and field values in the lineup editor; a means Approve, all-projects on Decisions and Add on Welcome. Owner 2026-10-25 chose: [ and ] switch projects everywhere (wrapping through Needs you as left/right do today); left/right then belong to in-view tabs and fields (Decisions tabs stop being an exception); a stays Approve, A toggles all projects on Decisions, + adds a project on Welcome; the n and p row aliases are dropped (n is No in confirms); ctrl+d and ctrl+u scroll an open pane. Revises the key part of records 112 and 120.",
  "options": [
    {
      "label": "brackets",
      "summary": "[ and ] switch projects; left/right are for in-view tabs and fields; a, A, + split; n and p aliases dropped",
      "tradeoffs": "One consistent meaning per key; changes habits and docs; one slice for the keymap."
    },
    {
      "label": "keep",
      "summary": "Keep left/right as project keys; only split a into a, A and +",
      "tradeoffs": "Smaller change; left/right stay overloaded."
    }
  ],
  "recommendation": {
    "label": "brackets",
    "reasoning": "Owner 2026-10-25 after the design review."
  },
  "decision": "brackets",
  "rationale": "Owner 2026-10-25: [ and ] switch projects; left/right for in-view tabs and fields; a/A/+ split; n and p aliases dropped. Revises 112 and 120 keys.",
  "refs": [
    "S85",
    "adj-112",
    "adj-120"
  ],
  "history": [
    {
      "date": "2026-10-25",
      "event": "ask"
    },
    {
      "date": "2026-10-25",
      "event": "decide"
    }
  ]
}
---
