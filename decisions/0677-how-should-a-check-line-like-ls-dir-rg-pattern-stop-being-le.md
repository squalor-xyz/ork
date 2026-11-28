---
{
  "id": 677,
  "title": "How should a Check line like ls DIR | rg PATTERN stop being left to the orchestrator?",
  "status": "decided",
  "key": "checks.read-only-pipes",
  "asked_by": "Jon",
  "decided_by": "Jon",
  "created": "2026-11-27",
  "decided_at": "2026-11-27",
  "context": "S524 spec check.",
  "options": [
    {
      "label": "ork-runs-pipe",
      "summary": "ork runs a pipeline of read-only commands such as ls and rg itself, with no shell, like && chains (extends adj 454)"
    },
    {
      "label": "rewrite",
      "summary": "rewrite lines"
    },
    {
      "label": "implementer-runnable",
      "summary": "implementer-runnable"
    }
  ],
  "recommendation": {
    "label": "ork-runs-pipe",
    "reasoning": "Recommended in the interactive question."
  },
  "decision": "ork-runs-pipe",
  "rationale": "Owner chose 'ork runs read-only pipes' in an interactive question, relay session 8d711f67, 2026-11-27.",
  "refs": [
    "S524"
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
