---
{
  "id": 494,
  "title": "Cloud claude implementers never run the approval hook, so how far does the recursive-rm and outside-worktree guard reach?",
  "status": "decided",
  "key": "launch.rm-guard-scope",
  "asked_by": "Jon",
  "decided_by": "Jon",
  "created": "2026-11-18",
  "decided_at": "2026-11-18",
  "context": "Question: Cloud claude implementers never run the approval hook, so how far does the recursive-rm and outside-worktree guard reach?\nOptions:\n1. local only: build the hook guard as written (step 3, `internal/agent/claude/pretool.go`); the docs and acceptance criteria say it holds for claude-local only. A cloud claude implementer's `--allowedTools` entry `Bash(rm *)` then also allows `rm -r dir` and `rm ../x`, and codex and grok implementers are not covered either.\n2. cloud deny rules: also pass deny rules (for example `--disallowedTools` for `rm -r*`) in `internal/agent/claude/args.go` for cloud runs. This is a new arg path and the CLI's deny-rule behaviour has not been probed.\n3. drop plain rm: keep `git rm` and drop `Bash(rm *)` from the presets, which changes adj 478's \"all\" and leaves the S350 untracked-stray-file case with no allowed form.\nRecommend: 1 - it is the narrowest change and matches the spec's step 3. Options 2 and 3 are the owner's call because the first adds a new component and the second changes adj 478.\nDecision key: launch.rm-guard-scope\n\nI checked the rest against the tree and it needs nothing more from the owner:\n- adj 478 is decided: `launch.allowlist-move` = all.\n- The preset base is in `internal/engine/perm.go:45-69`. `perm_test.go` has the exact-list test at `:13`, and `partAllowlist` keeps non-slicer entries.\n- The doc anchors exist: `docs/design.md` preset lists, the `--preset` row in `docs/guide.md`, and `docs/state.md:161`.\n- `args.go:137` installs the hook only for claude-local; for cloud claude, `args.go:134` passes the allowlist as `--allowedTools` with `--permission-prompts none`.\n- The existing path rule denies `rm ../other/file`, `rm /home/x/file` and `git -C /elsewhere rm f` with \"outside worktree\" text, so the denial test is feasible.\n- Three small gaps I closed by reading the code, so they are not questions:\n- Step 3's \"vet git mv's options the way adj 457 did for rg\" has no code to copy. adj 457 only dropped `rg`, and `git mv` and `git ls-files` have no program-running option.\n- The recursive-rm test says \"not allowed\", so either `ask` or `deny` passes.\n- The new `pretool_test.go` tests should build their allowlist literal in the test, as `TestPretoolPathsAndAllowlist` does.",
  "decision": "cloud deny rules: also pass deny rules (for example `--disallowedTools` for `rm -r*`) in `internal/agent/claude/args.go` for cloud runs. This is a new arg path and the CLI's deny-rule behaviour has not been probed.",
  "rationale": "Answered in ork on 2026-11-18: Cloud claude implementers never run the approval hook, so how far does the recursive-rm and outside-worktree guard reach? (S345)",
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
