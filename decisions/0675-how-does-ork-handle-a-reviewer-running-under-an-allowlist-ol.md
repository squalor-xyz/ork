---
{
  "id": 675,
  "title": "How does ork handle a reviewer running under an allowlist older than the slice's own permission change?",
  "status": "decided",
  "key": "brief.reviewer-allowlist-build",
  "asked_by": "Jon",
  "decided_by": "Jon",
  "created": "2026-11-27",
  "decided_at": "2026-11-27",
  "context": "S463 spec check.",
  "options": [
    {
      "label": "brief-note",
      "summary": "the reviewer brief says its permissions come from the running ork build, so the slice's own permission change is not in effect; ask the orchestrator to run denied commands"
    },
    {
      "label": "fresh-build",
      "summary": "fresh build"
    },
    {
      "label": "retire",
      "summary": "retire"
    }
  ],
  "recommendation": {
    "label": "brief-note",
    "reasoning": "Recommended in the interactive question."
  },
  "decision": "brief-note",
  "rationale": "Owner chose 'Brief note' in an interactive question, relay session 8d711f67, 2026-11-27.",
  "refs": [
    "S463"
  ],
  "history": [
    {
      "date": "2026-11-27",
      "event": "ask"
    },
    {
      "date": "2026-11-27",
      "event": "decide"
    }
  ]
}
---
