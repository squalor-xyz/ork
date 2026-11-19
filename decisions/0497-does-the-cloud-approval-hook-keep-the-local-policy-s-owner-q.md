---
{
  "id": 497,
  "title": "Does the cloud approval hook keep the local policy's owner questions and Read cap?",
  "status": "decided",
  "key": "launch.cloud-hook-scope",
  "asked_by": "Jon",
  "decided_by": "Jon",
  "created": "2026-11-19",
  "decided_at": "2026-11-19",
  "context": "adj 496 runs the approval hook for cloud Claude implementers; the local policy asks the owner on uncovered calls and caps Read at 150 lines (run ork-20261118-225932-056b).",
  "options": [
    {
      "label": "paths",
      "summary": "Cloud hook denies outside-worktree paths and recursive rm, turns ask into deny, no Read cap"
    },
    {
      "label": "whole",
      "summary": "Cloud matches claude-local, including owner questions and the Read cap"
    },
    {
      "label": "cap",
      "summary": "Keep uncovered calls denied but apply the Read cap"
    }
  ],
  "recommendation": {
    "label": "paths",
    "reasoning": "Recommended in the interactive question."
  },
  "decision": "paths",
  "rationale": "Owner chose 'Paths only on cloud (Recommended)' in an interactive question, relay session 8d711f67, 2026-11-19.",
  "refs": [
    "S345"
  ],
  "history": [
    {
      "date": "2026-11-19",
      "event": "ask"
    },
    {
      "date": "2026-11-19",
      "event": "decide"
    }
  ]
}
---
