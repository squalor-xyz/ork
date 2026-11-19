---
{
  "id": 496,
  "title": "How does cloud Claude block an rm of a path outside the worktree?",
  "status": "decided",
  "key": "launch.cloud-remove-paths",
  "asked_by": "Jon",
  "decided_by": "Jon",
  "created": "2026-11-18",
  "decided_at": "2026-11-18",
  "context": "adj 494 added cloud recursive-rm deny rules; the approval hook that checks roots and symlinks runs only for claude-local (run ork-20261118-225932-056b).",
  "options": [
    {
      "label": "hook",
      "summary": "Run the existing approval hook for cloud Claude implementers too, keeping the recursive deny rules"
    },
    {
      "label": "deny",
      "summary": "Deny absolute and ../ rm forms on cloud"
    },
    {
      "label": "local",
      "summary": "Outside-worktree checks stay claude-local only"
    }
  ],
  "recommendation": {
    "label": "hook",
    "reasoning": "Recommended in the interactive question."
  },
  "decision": "hook",
  "rationale": "Owner chose 'Extend the hook to cloud (Implementer's pick)' in an interactive question, relay session 8d711f67, 2026-11-18.",
  "refs": [
    "S345"
  ],
  "history": [
    {
      "date": "2026-11-18",
      "event": "ask"
    },
    {
      "date": "2026-11-18",
      "event": "decide"
    }
  ]
}
---
