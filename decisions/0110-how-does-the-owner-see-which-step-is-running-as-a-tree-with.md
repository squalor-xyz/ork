---
{
  "id": 110,
  "title": "How does the owner see which step is running, as a tree with a flow diagram?",
  "status": "decided",
  "key": "ui.run-tree",
  "asked_by": "claude",
  "decided_by": "jon",
  "created": "2026-10-20",
  "decided_at": "2026-10-20",
  "context": "Owner 2026-10-20: see what step is running under the sub-orchestrators, like a tree, ideally with an animated ASCII flow diagram. S07 already lists charts and animations but has no spec and waits on S05. The ledger records a parent per step (relay-run step --parent).",
  "options": [
    {
      "label": "tree-then-animate",
      "summary": "a tree slice now in the S26 pane; animation and flow diagram stay in S07",
      "tradeoffs": "two reviewable changes; tree is useful alone"
    },
    {
      "label": "one-slice",
      "summary": "tree and animated flow together",
      "tradeoffs": "bigger; more golden and tick work at once"
    },
    {
      "label": "extend-s07",
      "summary": "no new slice",
      "tradeoffs": "stays blocked on S05"
    }
  ],
  "recommendation": {
    "label": "tree-then-animate",
    "reasoning": "Delivers the tree without waiting on S05; animation builds on the same data."
  },
  "decision": "tree-then-animate",
  "rationale": "Owner 2026-10-20: a tree slice now, shown in the S26 detail pane (orchestrator, roles and steps with live status, from the ledger parents and engine roles); the animated flow diagram, stage bars and charts stay in S07.",
  "refs": [
    "S07",
    "S26",
    "S30"
  ],
  "history": [
    {
      "date": "2026-10-20",
      "event": "ask"
    },
    {
      "date": "2026-10-20",
      "event": "decide"
    }
  ]
}
---
