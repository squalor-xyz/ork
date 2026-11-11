---
{
  "id": 285,
  "title": "S333: how does the owner reach a stale ledger's pane and x end run once its row leaves the Running list?",
  "status": "decided",
  "key": "ui.stale-ledger-access",
  "asked_by": "claude",
  "decided_by": "Jon",
  "via": "interactive question, relay session 8d711f67, 2026-11-11",
  "created": "2026-11-11",
  "decided_at": "2026-11-11",
  "context": "S333 spec check 2026-11-11: adj 284 moves stale relay ledgers into one 'stale: N' line; S175's x end run lives only on a stale ledger pane (docs/dashboard.md:202-254).",
  "options": [
    {
      "label": "selectable",
      "summary": "the stale: N line is a cursor row; enter or space shows the N stale rows, each opens its pane as today"
    },
    {
      "label": "line-only",
      "summary": "plain dim line; the stale rows are listed in another view"
    },
    {
      "label": "cli-only",
      "summary": "plain count; end with slicer relay-run end outside ork"
    }
  ],
  "recommendation": {
    "label": "selectable",
    "reasoning": "Removes clutter and keeps the S175 end-run action."
  },
  "decision": "selectable",
  "rationale": "Owner picked Selectable line (recommended).",
  "refs": [
    "S333"
  ],
  "history": [
    {
      "date": "2026-11-11",
      "event": "ask"
    },
    {
      "date": "2026-11-11",
      "event": "decide"
    }
  ]
}
---
