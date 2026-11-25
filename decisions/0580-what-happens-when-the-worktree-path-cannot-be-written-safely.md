---
{
  "id": 580,
  "title": "What happens when the worktree path cannot be written safely into a rule?",
  "status": "decided",
  "key": "launch.git-c-unpinnable-path",
  "asked_by": "Jon",
  "decided_by": "Jon",
  "created": "2026-11-25",
  "decided_at": "2026-11-25",
  "context": "S586 spec draft: a comma splits --allowedTools and a * becomes a wildcard.",
  "options": [
    {
      "label": "drop-git-c",
      "summary": "drop the git -C entries and keep the plain git SUBCMD* entries"
    },
    {
      "label": "pause",
      "summary": "pause the run with a permissions error"
    },
    {
      "label": "pin-anyway",
      "summary": "write the raw path"
    }
  ],
  "recommendation": {
    "label": "drop-git-c",
    "reasoning": "Recommended in the interactive question."
  },
  "decision": "drop-git-c",
  "rationale": "Owner chose 'Drop git -C entries' in an interactive question, relay session 8d711f67, 2026-11-25.",
  "refs": [
    "S586"
  ],
  "history": [
    {
      "date": "2026-11-25",
      "event": "ask"
    },
    {
      "date": "2026-11-25",
      "event": "decide"
    }
  ]
}
---
