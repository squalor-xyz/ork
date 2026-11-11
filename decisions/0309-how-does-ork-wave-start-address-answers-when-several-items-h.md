---
{
  "id": 309,
  "title": "How does `ork wave start` address answers when several items have questions?",
  "status": "decided",
  "key": "specs.questions-wave-addressing",
  "asked_by": "Jon",
  "decided_by": "Jon",
  "created": "2026-11-11",
  "decided_at": "2026-11-11",
  "context": "Question: How does `ork wave start` address answers when several items have questions?\nOptions:\n1. item prefix: `--answer ITEM:KEY=N` and `ITEM:#N` on `ork wave start`. `ork run` keeps `KEY=N` and `#N`, since it has one item.\n2. run only: add `--questions` and `--answer` to `ork run` only. `ork wave start` keeps the prompt, and a later slice can extend it.\n3. bare keys: `KEY=N` and `#N` apply to every item in the wave. A key shared by two items, or a `#N`, is refused as ambiguous.\nRecommend: 1 - `startWave` spec-checks several items in one call (`internal/cli/wave.go:~355`). `#N` and any shared key are ambiguous without an item. The failing tests cover only `ork run`.\nDecision key: specs.questions-wave-addressing",
  "decision": "item prefix: `--answer ITEM:KEY=N` and `ITEM:#N` on `ork wave start`. `ork run` keeps `KEY=N` and `#N`, since it has one item.",
  "rationale": "Answered in ork on 2026-11-11: How does `ork wave start` address answers when several items have questions?",
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
    }
  ]
}
---
