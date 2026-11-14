---
{
  "id": 345,
  "title": "How does the approval point decide that a Bash call \"names a path outside the worktree\"?",
  "status": "decided",
  "key": "roles.local-approval-bash-paths",
  "asked_by": "Jon",
  "decided_by": "Jon",
  "created": "2026-11-13",
  "decided_at": "2026-11-13",
  "context": "Question: How does the approval point decide that a Bash call \"names a path outside the worktree\"?\nOptions:\n1. token scan: split the command on whitespace and shell operators. Deny any token that is an absolute path or contains `..`, unless it is under the worktree, the main checkout's `.scratch/`, the S358 progress directory (`Spec.AddDir`) or the S360 `tool-results` directory. Over-blocking is accepted, and the denial reason names the token.\n2. allowlist patterns only: Bash is allowed only when an allowlist pattern covers it, and the outside-path rule applies to `Read`, `Grep`, `Glob` and `Edit` alone. Rule 3 then decides Bash, and nothing parses commands.\n3. as written: the implementer chooses the matching rule.\nRecommend: 1 - it gives the test \"denies a `Read` or `Bash` naming a path outside the worktree\" a defined rule. The command text is untrusted model output, so the parsing tradeoff is the owner's.\nDecision key: roles.local-approval-bash-paths",
  "decision": "token scan: split the command on whitespace and shell operators. Deny any token that is an absolute path or contains `..`, unless it is under the worktree, the main checkout's `.scratch/`, the S358 progress directory (`Spec.AddDir`) or the S360 `tool-results` directory. Over-blocking is accepted, and the denial reason names the token.",
  "rationale": "Answered in ork on 2026-11-13: How does the approval point decide that a Bash call \"names a path outside the worktree\"? (S356)",
  "refs": [
    "S356"
  ],
  "history": [
    {
      "date": "2026-11-13",
      "event": "ask"
    },
    {
      "date": "2026-11-13",
      "event": "decide"
    }
  ]
}
---
