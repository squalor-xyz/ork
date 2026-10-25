---
{
  "id": 169,
  "title": "Where does the Running entry sit in the sidebar?",
  "status": "decided",
  "key": "ui.sidebar-running",
  "asked_by": "claude",
  "decided_by": "jon",
  "created": "2026-10-25",
  "decided_at": "2026-10-25",
  "context": "S110. Decided #82 (ui.project-nav) puts a Needs you entry first in the sidebar; S110 adds a Running entry above it. The owner chose reuse-overview in the 2026-10-25 batch spec check (AskUserQuestion); recording it needs the owner's decide and revises #82's order.",
  "options": [
    {
      "label": "reuse-overview",
      "summary": "a Running entry above Needs you opens the aggregate Overview's Running section full-screen with question and stop alerts; reached with up/down and enter (no digit until S85/S105); always shown, dim when nothing runs; 1 s refresh only for projects with a live run while visible"
    },
    {
      "label": "new-view",
      "summary": "a separate summary view as first specified"
    }
  ],
  "recommendation": {
    "label": "reuse-overview",
    "reasoning": "One Running row builder, less new code, and sidebar rows do not shift when a run starts."
  },
  "decision": "reuse-overview",
  "rationale": "Owner approved the recommendation in the agent session on 2026-10-25 (AskUserQuestion: yes, decide all 162-170); recorded by the agent at the owner's direction. The owner picked this option in the 2026-10-25 batch spec check. Revises #82 only in the sidebar order (Running above Needs you); the rest of #82 stands.",
  "refs": [
    "S110",
    "ui.project-nav"
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
