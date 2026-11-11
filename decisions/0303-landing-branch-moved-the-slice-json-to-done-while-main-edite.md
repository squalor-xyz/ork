---
{
  "id": 303,
  "title": "Landing: branch moved the slice JSON to done/ while main edited the old path. What does ork do?",
  "status": "decided",
  "key": "landing.deleted-slice-json",
  "asked_by": "claude",
  "decided_by": "Jon",
  "via": "interactive question, relay session 8d711f67, 2026-11-11",
  "created": "2026-11-11",
  "decided_at": "2026-11-11",
  "options": [
    {
      "label": "resolve",
      "summary": "Three-way merge main's change into the done/ copy; clean means git rm the old path and go on, conflict pauses naming the done/ file"
    },
    {
      "label": "pause-commands",
      "summary": "Pause with one line naming the move and the two commands"
    },
    {
      "label": "keep-branch",
      "summary": "Always keep the branch's done/ copy and drop main's edit"
    }
  ],
  "recommendation": {
    "label": "resolve",
    "reasoning": "S328 and S217 both needed the same hand fix"
  },
  "decision": "resolve",
  "rationale": "Owner picked resolve it itself",
  "refs": [
    "S334"
  ],
  "history": [
    {
      "date": "2026-11-11",
      "event": "ask"
    },
    {
      "date": "2026-11-11",
      "event": "decide"
    }
  ]
}
---
