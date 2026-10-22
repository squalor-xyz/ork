---
{
  "id": 148,
  "title": "Does a UI-touching slice get a UI pass in relays and waves?",
  "status": "open",
  "key": "relay.ui-pass",
  "tags": [
    "relay",
    "ui"
  ],
  "created": "2026-10-22",
  "context": "Owner asked (2026-10-22) that any slice touching or addressing the UI get a UI-subagent pass in relays and waves, at opus/medium. slicer's role matrix accepts only implementer, reviewer, gate, architect and orchestrator, so a UI role must reuse one of them. Scope: internal/tui and golden views, docs/dashboard.md, internal/cli output, any ui.* adj key.",
  "options": [
    {
      "label": "reviewer-row",
      "summary": "Matrix Reviewer row r14 (opus, medium, note UI pass) after r5, plus rule u10; a general-purpose subagent loads the frontend-design skill, edits nothing, and returns suggested design edits via the orchestrator to the implementer; the gate decides",
      "tradeoffs": "Fits the existing matrix; recommend still picks r5, so the extra pass is applied by rule u10 and judgment, not by recommend"
    },
    {
      "label": "architect-row",
      "summary": "An Architect row (opus, medium) for the UI pass",
      "tradeoffs": "Architect already runs in parallel, but is easy to confuse with the size-L architect r7"
    },
    {
      "label": "agents-only",
      "summary": "Rule in AGENTS.md only, no matrix row",
      "tradeoffs": "Lineup proposals from the matrix would not show the UI pass"
    }
  ],
  "recommendation": {
    "label": "reviewer-row",
    "reasoning": "Owner chose it in session; keeps the lineup visible in relay-roles show"
  },
  "refs": [
    "AGENTS.md"
  ],
  "history": [
    {
      "date": "2026-10-22",
      "event": "ask"
    }
  ]
}
---
