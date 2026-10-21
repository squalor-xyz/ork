---
{
  "id": 134,
  "title": "How do launch edits travel on the wire to run.plan, run.start and wave.start?",
  "status": "decided",
  "key": "run.launch-wire",
  "asked_by": "claude",
  "decided_by": "Jon R",
  "created": "2026-10-20",
  "decided_at": "2026-10-20",
  "context": "RunParams is {project,item,well_specified,split,expected} today (internal/daemon/runs.go:40-46). The daemon refuses a start whose plan differs from Expected (runs.go:275-281).",
  "options": [
    {
      "label": "full",
      "summary": "Optional lineup (the whole entry list per role) and budgets {minutes,tokens}; the plan echoes them so the Expected check covers them; wave.start items carry the same fields"
    },
    {
      "label": "delta",
      "summary": "Optional overrides with only the changed roles and fields, merged by the daemon over the recommend result"
    }
  ],
  "recommendation": {
    "label": "full",
    "reasoning": "One shape in params, plan and run.json; the Expected check compares like with like."
  },
  "decision": "full",
  "rationale": "Owner accepted the recommendation (2026-10-20).",
  "refs": [
    "S80",
    "S83"
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
