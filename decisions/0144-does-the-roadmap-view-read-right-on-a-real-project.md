---
{
  "id": 144,
  "title": "Does the Roadmap view read right on a real project?",
  "status": "decided",
  "key": "review.s12-roadmap",
  "asked_by": "claude",
  "decided_by": "Jon R",
  "created": "2026-10-21",
  "decided_at": "2026-10-31",
  "context": "Open the Roadmap view (tab) on the slicer project and on adjudicate. Design choices to look at: a failed slicer list read hides the last good item list while a Deps error keeps rows and shows ?; ork status now also triggers a Deps read; item.show runs slicercli.Show outside the cache's 4-process cap (RecommendRoles does the same).",
  "options": [
    {
      "label": "ok",
      "summary": "Roadmap reads right",
      "tradeoffs": "Closes the S12 human check."
    },
    {
      "label": "fix",
      "summary": "Needs changes; file a follow-up",
      "tradeoffs": "Costs a follow-up."
    }
  ],
  "recommendation": {
    "label": "ok",
    "reasoning": "Gate passed; goldens and tests cover the layout."
  },
  "decision": "ok",
  "rationale": "Owner picked this option in an interactive question in the v1.0 release session on 2026-10-31 (AskUserQuestion), recommendation followed.",
  "refs": [
    "S12"
  ],
  "history": [
    {
      "date": "2026-10-21",
      "event": "ask"
    },
    {
      "date": "2026-10-31",
      "event": "decide"
    }
  ]
}
---
