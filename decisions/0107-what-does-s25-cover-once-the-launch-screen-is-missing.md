---
{
  "id": 107,
  "title": "What does S25 cover once the launch screen is missing?",
  "status": "decided",
  "key": "ui.launch-split",
  "asked_by": "claude",
  "decided_by": "jon",
  "created": "2026-10-20",
  "decided_at": "2026-10-20",
  "context": "S25 preflight: launch has no screen to pick a slice from (Roadmap is S12) and no definition of ready. The launch confirm should show the plan.",
  "options": [
    {
      "label": "split",
      "summary": "S25 is approve, stop and retry; launch is its own slice",
      "tradeoffs": "one reviewable change; launch waits for a list surface"
    },
    {
      "label": "list",
      "summary": "S25 also adds a launch list",
      "tradeoffs": "larger; overlaps S12"
    },
    {
      "label": "typed-id",
      "summary": "launch asks for a typed slice ID",
      "tradeoffs": "no list or ready rule; weaker UX"
    }
  ],
  "recommendation": {
    "label": "split",
    "reasoning": "Keeps S25 shippable now."
  },
  "decision": "split",
  "rationale": "Owner 2026-10-20: S25 = approve, stop, retry. Launch is its own slice (ready = open, has a spec, no claim, no unmet dependency). The launch confirm shows the plan summary and the exact command (run.plan, then run.start with Expected).",
  "supersedes": [
    103
  ],
  "refs": [
    "S25",
    "103"
  ],
  "history": [
    {
      "date": "2026-10-20",
      "event": "ask"
    },
    {
      "date": "2026-10-20",
      "event": "decide"
    },
    {
      "date": "2026-10-20",
      "event": "supersede",
      "note": "supersedes 103"
    }
  ]
}
---
