---
{
  "id": 553,
  "title": "Where does the `wave:` line go relative to the `next:` line?",
  "status": "decided",
  "key": "ui.status-wave-line-order",
  "asked_by": "Jon",
  "decided_by": "Jon",
  "created": "2026-11-25",
  "decided_at": "2026-11-25",
  "context": "Question: Where does the `wave:` line go relative to the `next:` line?\nOptions:\n1. after-next: print the question, the options, the `next:` line, then `wave:`. The `next:` command stays attached to its options, and the wave comes last.\n2. before-next: print the question, the options, `wave:`, then the `next:` line. This splits the options from their `next:` command, the same way the wave line splits the question from its options today.\n3. one-line-only: print the wave right after the row when the wait has one line or none, and use order 1 for a multi-line wait.\nRecommend: 1 - the `next:` line belongs to the wait, and the slice only asks for the wave to come after the options.\nDecision key: ui.status-wave-line-order\n\nThe wave placement is not decided in adj: `adj check \"ui.status-wave-line-order\"` returns `not found`. Two places state the old order and must change with the code:\n- `docs/design.md:3840` says the wave line prints \"before the question lines\".\n- `docs/dashboard.md:996` also describes the order.",
  "decision": "after-next: print the question, the options, the `next:` line, then `wave:`. The `next:` command stays attached to its options, and the wave comes last.",
  "rationale": "Answered in ork on 2026-11-25: Where does the `wave:` line go relative to the `next:` line? (S553)",
  "refs": [
    "S553"
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
