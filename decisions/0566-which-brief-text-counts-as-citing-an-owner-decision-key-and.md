---
{
  "id": 566,
  "title": "Which brief text counts as citing an owner decision key, and what does ork say when `adj check` finds nothing?",
  "status": "decided",
  "key": "brief.decision-key-check",
  "asked_by": "Jon",
  "decided_by": "Jon",
  "created": "2026-11-25",
  "decided_at": "2026-11-25",
  "context": "Question: Which brief text counts as citing an owner decision key, and what does ork say when `adj check` finds nothing?\nOptions:\n1. owner-decisions: only the `[key]` and `[key, adj N]` markers in the \"Owner decisions\" paragraph that `RecordSpecAnswers` writes (`internal/engine/speccheck.go:840-846`, carried into briefs at `internal/engine/round.go:662`). For each key without an adj id, ork runs `adj check KEY` and appends a line such as \"adj check KEY: not found\" to that brief section. Keys already carrying `adj N` are not re-checked.\n2. any-key: scan the whole brief text, including the slice's sections, for dotted `area.topic` keys, and run `adj check` on each. This catches a key an agent or the owner typed into a spec by hand. It needs a pattern for what a key looks like, and false hits are possible.\n3. owner-decisions-and-block: as option 1, but a key that is not found also pauses the run for the owner. This is heavier, and the slice's Why asks only to \"say in the brief\".\nRecommend: 1 - the Why's evidence is a key in the Owner decisions paragraph, ork already writes those markers, and the brief only notes the result.\nDecision key: brief.decision-key-check",
  "decision": "owner-decisions: only the `[key]` and `[key, adj N]` markers in the \"Owner decisions\" paragraph that `RecordSpecAnswers` writes (`internal/engine/speccheck.go:840-846`, carried into briefs at `internal/engine/round.go:662`). For each key without an adj id, ork runs `adj check KEY` and appends a line such as \"adj check KEY: not found\" to that brief section. Keys already carrying `adj N` are not re-checked.",
  "rationale": "Answered in ork on 2026-11-25: Which brief text counts as citing an owner decision key, and what does ork say when `adj check` finds nothing? (S566)",
  "refs": [
    "S566"
  ],
  "history": [
    {
      "date": "2026-11-25",
      "event": "ask"
    },
    {
      "date": "2026-11-25",
      "event": "decide"
    }
  ]
}
---
