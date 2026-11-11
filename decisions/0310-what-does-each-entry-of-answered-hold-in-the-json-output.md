---
{
  "id": 310,
  "title": "What does each entry of `answered` hold in the `--json` output?",
  "status": "superseded",
  "key": "specs.questions-json-shape",
  "asked_by": "Jon",
  "decided_by": "Jon",
  "created": "2026-11-11",
  "decided_at": "2026-11-11",
  "context": "Question: What does each entry of `answered` hold in the `--json` output?\nOptions:\n1. key and answer: `answered` entries are `{item, key, answer}`. `key` is `#N` for a keyless question, and `answer` is the recorded \"Label: summary\" or free text. `questions` reuses `engine.SpecQuestion` (`ask`, `key`, `text`).\n2. position only: `answered` is a plain array of answer strings in question order.\n3. as written: leave the entry shape to the implementer.\nRecommend: 1 - it matches the by-key addressing and lets a script check what was recorded.\nDecision key: specs.questions-json-shape\n\nClosed by reading the code. These are not questions:\n\n- **Flag shape and record (adj 224, 225):** both are decided. The flag shape is (a), `--questions` plus repeatable `--answer KEY=N`. `--questions` leaves no record.\n- **Exit code:** S257 is done and `internal/cli/cli.go:35` defines `ExitWaitQuestion = 3`. The spec's fallback \"ExitHeld until S257 lands\" no longer applies, and no `ExitHeld` symbol exists. Reuse code 3 for `--questions`. A held item stays `ExitFailure`, as the `ork run` path does today (`internal/cli/run.go:127-139`).\n- **Drifted anchors (the symbol decides):**\n- `askSpecQuestions` is at `internal/cli/wave.go:536`, not 312.\n- `RecordSpecAnswers` is at `internal/engine/speccheck.go:604`, not 545.\n- The spec result types are in `internal/daemon/speccheck.go:46-76`; `internal/daemon/wire.go` exists but doesn't hold them.\n- **Check line:** `go test ./...`, `go vet ./...`, `gofmt -l .`, `slicer check` and `adj lint` are the project's commands, and done means each exits 0. The \"UI pass (rule u10)\" line is not a command. `AGENTS.md` assigns the UI pass to the orchestrator as relay row r14, so that assignment is already recorded.\n- **Failing tests:** each bullet states an assertion. The project's convention for slice specs doesn't require test function names.",
  "decision": "key and answer: `answered` entries are `{item, key, answer}`. `key` is `#N` for a keyless question, and `answer` is the recorded \"Label: summary\" or free text. `questions` reuses `engine.SpecQuestion` (`ask`, `key`, `text`).",
  "rationale": "Answered in ork on 2026-11-11: What does each entry of `answered` hold in the `--json` output?",
  "superseded_by": 319,
  "refs": [
    "S312"
  ],
  "history": [
    {
      "date": "2026-11-11",
      "event": "ask"
    },
    {
      "date": "2026-11-11",
      "event": "decide"
    },
    {
      "date": "2026-11-11",
      "event": "supersede",
      "note": "by 319"
    }
  ]
}
---
