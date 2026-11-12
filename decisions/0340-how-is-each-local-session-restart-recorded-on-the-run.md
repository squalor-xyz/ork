---
{
  "id": 340,
  "title": "How is each local session restart recorded on the run?",
  "status": "decided",
  "key": "roles.local-restart-record",
  "asked_by": "Jon",
  "decided_by": "Jon",
  "created": "2026-11-12",
  "decided_at": "2026-11-12",
  "context": "Question: How is each local session restart recorded on the run?\nOptions:\n1. restarts list: add a `restarts` array to the role's `RoleRecord` (`internal/runrec/record.go`), one `{at, tokens}` entry per restart. The role keeps one row and the dashboard is unchanged. Document the field in `docs/state.md`.\n2. row per session: each fresh session writes its own `RoleRecord` (same role and round, new `Session` id). `Tokens` is the context size at which the earlier session was ended. The dashboard and the round-based counters may then show several rows for one round.\n3. run-level log: a new run-record field, separate from `Roles`, that lists restarts for the whole run.\nRecommend: 1 - it records time and token count without changing how rows and rounds are counted.\nDecision key: roles.local-restart-record\n\nFiles and gaps I closed by reading the code, so they are not questions:\n- Q1 and Q2 are decided (adj 335 `70k`, adj 336 `scratch-note`). Step 1 of Implement, \"adj ask Q1 and Q2\", is stale; skip it.\n- The slice's Files list omits `internal/runrec/record.go`, `docs/state.md`, `internal/daemon/runner.go` and the role prompt text under `internal/roles/prompts`. The role prompt needs the \"write a progress note after each step\" instruction. These are in-scope edits the implementer can make.\n- The `Write` tool is allowed to claude-local roles without a path limit (`internal/engine/perm.go:46`). The brief's workspace-limit line must name the progress-note path as an exception.\n- Per-turn tokens already arrive as `EventUsage` (`internal/agent/claude/stream.go:76-83`). `internal/daemon/runner.go:40-70` already sums input, output, cache read and cache creation tokens.\n- `LocalRoleAfter` (90 min, `internal/engine/watchdog.go:28`) is already an absolute limit on elapsed time. It counts both sessions as long as the watch is not reset on restart.",
  "decision": "restarts list: add a `restarts` array to the role's `RoleRecord` (`internal/runrec/record.go`), one `{at, tokens}` entry per restart. The role keeps one row and the dashboard is unchanged. Document the field in `docs/state.md`.",
  "rationale": "Answered in ork on 2026-11-12: How is each local session restart recorded on the run? (S358)",
  "refs": [
    "S358"
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
