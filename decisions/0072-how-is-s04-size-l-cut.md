---
{
  "id": 72,
  "title": "How is S04 (size L) cut?",
  "status": "decided",
  "key": "plan.s04-cut",
  "asked_by": "claude",
  "decided_by": "jon",
  "created": "2026-10-19",
  "decided_at": "2026-10-19",
  "context": "S04 is size L, effort 3; the fastest path to dogfooding.",
  "options": [
    {
      "label": "three",
      "summary": "S04 engine core, S16 Claude adapter, S17 CLI surface"
    },
    {
      "label": "two",
      "summary": "Engine plus adapter, then the surface"
    },
    {
      "label": "one",
      "summary": "S04 whole, with an architect"
    }
  ],
  "decision": "three",
  "rationale": "Owner 2026-10-19: three slices: S04 engine core on a fake runner, S16 Claude adapter in parallel, S17 CLI surface after both; Claude only until S06.",
  "refs": [
    "S04"
  ],
  "history": [
    {
      "date": "2026-10-19",
      "event": "ask"
    },
    {
      "date": "2026-10-19",
      "event": "decide"
    }
  ]
}
---
