---
{
  "id": 339,
  "title": "How is a local role's long command output capped?",
  "status": "decided",
  "key": "roles.local-output-cap",
  "asked_by": "Jon",
  "decided_by": "Jon",
  "created": "2026-11-12",
  "decided_at": "2026-11-12",
  "context": "Question: How is a local role's long command output capped?\nOptions:\n1. env: set a Bash output limit in the local role's environment. The implementer finds the CLI's variable name and confirms it by measurement. The code change goes in `claudeEnv` in `internal/daemon/runner.go`. If no such variable exists, the slice stops and files a note for the owner.\n2. tail: change the brief so check commands pipe through `tail`. This edits shared brief text in `internal/engine/brief.go`, which is not in Files. The pipe also hides the check command's exit status.\n3. flags only: this slice ships the flags only, and the output cap becomes a separate slice.\nRecommend: 1 - it changes neither the brief nor the exit status the roles rely on, and it touches only local roles. The Failing test for the cap names no mechanism and no assertion until someone measures, and `claude --help` lists no flag for it.\nDecision key: roles.local-output-cap",
  "decision": "env: set a Bash output limit in the local role's environment. The implementer finds the CLI's variable name and confirms it by measurement. The code change goes in `claudeEnv` in `internal/daemon/runner.go`. If no such variable exists, the slice stops and files a note for the owner.",
  "rationale": "Answered in ork on 2026-11-12: How is a local role's long command output capped? (S360)",
  "refs": [
    "S360"
  ],
  "history": [
    {
      "date": "2026-11-12",
      "event": "ask"
    },
    {
      "date": "2026-11-12",
      "event": "decide"
    }
  ]
}
---
