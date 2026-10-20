---
{
  "id": 105,
  "title": "How is a denied command recorded on a role?",
  "status": "decided",
  "key": "engine.denied-command",
  "asked_by": "claude",
  "decided_by": "jon",
  "created": "2026-10-20",
  "decided_at": "2026-10-20",
  "context": "S45 orchestrator found the command is dropped at internal/daemon/runner.go:39-40 and Result.Denials is []string (internal/engine/runner.go:58). runrec rejects unknown fields (store.go:130).",
  "options": [
    {
      "label": "add-field",
      "summary": "RoleRecord.Denials stays names; a new omitempty field holds {tool, command}; Version stays 1",
      "tradeoffs": "old records load; a pre-S45 ork refuses a run.json with a command"
    },
    {
      "label": "objects",
      "summary": "Denials becomes objects with a loader for the old string form",
      "tradeoffs": "cleaner; larger compatibility surface"
    },
    {
      "label": "bump",
      "summary": "Version 2 with a migration",
      "tradeoffs": "heaviest"
    }
  ],
  "recommendation": {
    "label": "add-field",
    "reasoning": "Smallest change that keeps old records loading."
  },
  "decision": "add-field",
  "rationale": "Owner 2026-10-20: add an optional field; cap is 200 characters in total including the ASCII ellipsis; shown inline per denial; the recorded text is tool_input.command, else file_path, never file content; CR and LF become spaces before the cap.",
  "refs": [
    "S45",
    "93"
  ],
  "history": [
    {
      "date": "2026-10-20",
      "event": "ask"
    },
    {
      "date": "2026-10-20",
      "event": "decide"
    }
  ]
}
---
