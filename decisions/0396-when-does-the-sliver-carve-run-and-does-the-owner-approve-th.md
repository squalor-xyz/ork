---
{
  "id": 396,
  "title": "When does the sliver carve run, and does the owner approve the plan before ork files the slivers?",
  "status": "decided",
  "key": "roles.local-sliver-carve-timing",
  "asked_by": "Jon",
  "decided_by": "Jon",
  "created": "2026-11-16",
  "decided_at": "2026-11-16",
  "context": "S359 spec check question, 2026-11-16. The orchestrator first fed answer 1 (before-plan) by mistake with piped input; the run was stopped after about a minute and the owner was asked.",
  "options": [
    {
      "label": "before-plan",
      "summary": "ork run on a local lineup carves and files the slivers on main first, then shows the chain plan for the owner's one approval"
    },
    {
      "label": "in-spec-check",
      "summary": "the carve runs inside the spec check that ork runs before every start; ork files the slivers from its report and the chain plan lists them for the owner's one approval (adj 388)"
    }
  ],
  "recommendation": {
    "label": "before-plan"
  },
  "decision": "in-spec-check",
  "rationale": "Owner chose 'In spec check' in an interactive question, relay session 8d711f67, 2026-11-16, after the orchestrator reported its mistaken piped answer.",
  "refs": [
    "S359"
  ],
  "history": [
    {
      "date": "2026-11-16",
      "event": "ask"
    },
    {
      "date": "2026-11-16",
      "event": "decide"
    }
  ]
}
---
