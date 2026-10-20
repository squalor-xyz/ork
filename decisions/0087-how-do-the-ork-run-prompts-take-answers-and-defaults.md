---
{
  "id": 87,
  "title": "How do the ork run prompts take answers and defaults?",
  "status": "decided",
  "key": "run.prompt-answers",
  "asked_by": "claude",
  "decided_by": "jon",
  "created": "2026-10-19",
  "decided_at": "2026-10-19",
  "context": "S21: the prompts in internal/cli/run.go accept only yes or no. Well-specified and split are judgements (relay-roles u4); launch is paid.",
  "options": [
    {
      "label": "confirm-no",
      "summary": "accept y/yes/n/no in any case; the confirm prompt defaults to No on Enter; well-specified and split have no default"
    },
    {
      "label": "split-no",
      "summary": "as confirm-no, and split also defaults to No"
    }
  ],
  "decision": "confirm-no",
  "rationale": "Owner 2026-10-20: accept yes, y, no, n in any case. The confirm prompt defaults to No on Enter. Well-specified and split have no default.",
  "refs": [
    "S21"
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
