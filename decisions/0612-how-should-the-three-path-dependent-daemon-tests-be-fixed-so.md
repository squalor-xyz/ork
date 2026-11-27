---
{
  "id": 612,
  "title": "How should the three PATH-dependent daemon tests be fixed so the check passes?",
  "status": "decided",
  "key": "tests.check-command-host-binaries",
  "asked_by": "Jon",
  "decided_by": "Jon",
  "created": "2026-11-27",
  "decided_at": "2026-11-27",
  "context": "Context: the daemon tests `TestProjectsSetSetsAndClearsEveryField`, `TestProjectsSetRoundCheckNeedsACheckCommand` and `TestProjectsSetConcurrentCallsKeepEveryChange` need `make`, `full` and `fast` on PATH (commit 5e31b4ac verifies the check command where it runs). The check host lacks `make`, so `go test ./...` fails on them whatever the slice changes.\n\nQuestion: How should the three PATH-dependent daemon tests be fixed so the check passes?\nOptions:\n1. Install make on the check host: a host fix only; `full` and `fast` would still be missing, so `TestProjectsSetRoundCheckNeedsACheckCommand` keeps failing.\n2. Fix the tests in a separate slice: use a command that exists on any host (such as `go`) or stub PATH in the tests, so they no longer depend on the host.\n3. Accept the failure for S614: land it with the check overridden for these three tests, and leave them for later.\nRecommend: 2 - the tests should not depend on host binaries; it fixes all three tests, and S614 stays scoped to landing and run start.\nDecision key: tests.check-command-host-binaries",
  "decision": "Neither option: the premise is wrong. The three tests pass on main without make, full or fast on PATH, because projects set verifies the check command only when the project path is an existing directory (internal/daemon/handlers.go:940, verifyNewCheck). S614's own change to runDaemonIn in internal/daemon/runs_test.go creates p.Path/.git with os.MkdirAll, which makes p.Path exist, so verification now runs and fails. Fix it inside S614: route git rev-parse --git-common-dir to a directory under the test root (for example filepath.Join(root, \"gitdir\")), create that one instead, and do not create p.Path. The three tests then pass unchanged. (Jon chose 'Fix in S614's helper', session 8d711f67, 2026-11-27)",
  "rationale": "Answered in ork on 2026-11-27: How should the three PATH-dependent daemon tests be fixed so the check passes?",
  "refs": [
    "S614"
  ],
  "history": [
    {
      "date": "2026-11-27",
      "event": "ask"
    },
    {
      "date": "2026-11-27",
      "event": "decide"
    }
  ]
}
---
