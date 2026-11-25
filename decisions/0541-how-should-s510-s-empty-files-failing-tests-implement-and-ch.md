---
{
  "id": 541,
  "title": "How should S510's empty Files, Failing tests, Implement and Check be filled in, and which roles get a temp-dir write grant?",
  "status": "decided",
  "key": "roles.read-only-temp-write",
  "asked_by": "Jon",
  "decided_by": "Jon",
  "created": "2026-11-24",
  "decided_at": "2026-11-24",
  "context": "Question: How should S510's empty Files, Failing tests, Implement and Check be filled in, and which roles get a temp-dir write grant?\nOptions:\n1. reviewer-only: only the reviewer gets an Edit/Write grant limited to its own role temp dir (cloud Claude and claude-local). Files: `internal/agent/claude/args.go`, `internal/agent/claude/pretool.go` and the reviewer's brief wording. Failing tests: a new `args_test.go` test asserting the reviewer's args hold `--add-dir TMPDIR`, `Edit(//TMPDIR/**)` and `Write(//TMPDIR/**)`, and a `pretool_test.go` case that allows a Write under TempDir and denies one outside it. Check: `go test ./internal/agent/claude` exits 0.\n2. all-read-only: every read-only role (reviewer, gate, architect, spec check, spec draft) gets the same temp-dir grant. Tests and Check match option 1 and cover each role.\n3. prompt-only: leave the permission as it is and change the reviewer's brief so it no longer says it may write to its temp dir. The slice would change a prompt and a golden file.\nRecommend: 1 - the S449 evidence comes from the reviewer, and the grant stays as narrow as the report needs.\nDecision key: roles.read-only-temp-write",
  "decision": "reviewer-only: only the reviewer gets an Edit/Write grant limited to its own role temp dir (cloud Claude and claude-local). Files: `internal/agent/claude/args.go`, `internal/agent/claude/pretool.go` and the reviewer's brief wording. Failing tests: a new `args_test.go` test asserting the reviewer's args hold `--add-dir TMPDIR`, `Edit(//TMPDIR/**)` and `Write(//TMPDIR/**)`, and a `pretool_test.go` case that allows a Write under TempDir and denies one outside it. Check: `go test ./internal/agent/claude` exits 0.",
  "rationale": "Answered in ork on 2026-11-25: How should S510's empty Files, Failing tests, Implement and Check be filled in, and which roles get a temp-dir write grant? (S510)",
  "refs": [
    "S510"
  ],
  "history": [
    {
      "date": "2026-11-24",
      "event": "ask"
    },
    {
      "date": "2026-11-24",
      "event": "decide"
    }
  ]
}
---
