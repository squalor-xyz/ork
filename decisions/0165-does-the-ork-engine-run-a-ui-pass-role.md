---
{
  "id": 165,
  "title": "Does the ork engine run a UI pass role?",
  "status": "decided",
  "key": "roles.ui-pass",
  "asked_by": "claude",
  "decided_by": "jon",
  "created": "2026-10-25",
  "decided_at": "2026-10-25",
  "context": "Retrospective R4 (docs/review-2026-10-25-relay-retrospective.md). AGENTS.md runs a UI pass (matrix r14, rule u10) on UI slices in the hand-run relay; the engine has no such role and relay-run has no ui role (S87). The UI pass found layout problems tests passed (S33, S84) but read goldens only.",
  "options": [
    {
      "label": "ui-pass-with-renders",
      "summary": "for slices that touch the UI the engine runs a read-only UI pass in parallel with the reviewer, from the lineup's r14 row; it returns triaged findings and rendered views at 80x24, 120x40 and ASCII, shown on the approval card"
    },
    {
      "label": "none",
      "summary": "no UI pass in the engine"
    }
  ],
  "recommendation": {
    "label": "ui-pass-with-renders",
    "reasoning": "Keeps the gate that caught UI problems, and moves the owner's human check to approval time instead of an adj inbox."
  },
  "decision": "ui-pass-with-renders",
  "rationale": "Owner approved the recommendation in the agent session on 2026-10-25 (AskUserQuestion: yes, decide all 162-170); recorded by the agent at the owner's direction. ",
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
