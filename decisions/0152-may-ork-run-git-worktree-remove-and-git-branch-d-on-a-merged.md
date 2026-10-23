---
{
  "id": 152,
  "title": "May ork run git worktree remove and git branch -d on a merged slice branch after a confirm?",
  "status": "decided",
  "key": "git.cleanup-key",
  "decided_by": "Jon R",
  "created": "2026-10-23",
  "decided_at": "2026-10-23",
  "context": "Decisions 36 and 49 say ork detects merged branches and offers cleanup, and keeps a worktree until landed. docs/boundaries.md:17 says landing is outside ork. S63 would add one TUI key that, after a y/n confirm, runs git worktree remove then git branch -d (never --force or -D) for a committed run whose branch is merged. Owner chose this on 2026-10-23 in the interview; this record files it.",
  "options": [
    {
      "label": "confirmed-key",
      "summary": "One TUI key runs both commands after a confirm that shows them",
      "tradeoffs": "Saves the manual step; ork starts writing to git, only for a merged branch and never forced."
    },
    {
      "label": "copy-commands",
      "summary": "Show the commands to copy; ork never writes to git",
      "tradeoffs": "Keeps ork read-only on git; the owner types them."
    }
  ],
  "recommendation": {
    "label": "confirmed-key",
    "reasoning": "Owner chose it 2026-10-23; git branch -d refuses an unmerged branch, and the confirm shows both commands."
  },
  "decision": "confirmed-key",
  "rationale": "Owner 2026-10-23 interview: one key runs the cleanup after a confirm showing both commands.",
  "refs": [
    "S63",
    "S09"
  ],
  "history": [
    {
      "date": "2026-10-23",
      "event": "ask"
    },
    {
      "date": "2026-10-23",
      "event": "decide"
    }
  ]
}
---
