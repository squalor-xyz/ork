---
{
  "id": 154,
  "title": "Approve more paid codex calls to close the S48 sandbox gaps?",
  "status": "open",
  "key": "vendor.codex-sandbox-gaps",
  "asked_by": "implementer",
  "created": "2026-10-23",
  "context": "S48 used 3 approved invocations (2 reached the API). Probe 07 (socket bind) has no command event in its JSONL (testdata/vendors/codex/07-socket-bind.stdout), so under the S48 rule it does not count; the traceback is only the model's report. Also unmeasured: whether .git of a linked worktree is writable under workspace-write (probe 05 tried only the main checkout .git/slicer path), and loopback TCP bind (the python command died at the unix bind). Known so far: probe 05 shows a worktree write succeeds (the only command_execution event, 05-workspace-write-paths.stdout lines 4-5). Its reports that writes to the main checkout and its .git/slicer fail with 'operation not permitted' are the model's words in an agent_message with no command event, so they are unverified. Also unmeasured: that block.",
  "options": [
    {
      "label": "two",
      "summary": "Approve 2 calls: socket bind redo (unix and tcp as separate commands) and git add/commit inside the worktree, each prompt also re-measuring the main-checkout and .git/slicer write block with a command event",
      "tradeoffs": "Closes the gaps; costs 2 calls."
    },
    {
      "label": "none",
      "summary": "Accept the gaps; codex stays read-only in v1 (vendor.codex-allowlist)",
      "tradeoffs": "No spend; table keeps these cells unverified."
    }
  ],
  "recommendation": {
    "label": "none",
    "reasoning": "vendor.codex-allowlist already limits codex to read-only roles in v1, so these cells do not block v1."
  },
  "refs": [
    "S48",
    "S01"
  ],
  "history": [
    {
      "date": "2026-10-23",
      "event": "ask"
    }
  ]
}
---
