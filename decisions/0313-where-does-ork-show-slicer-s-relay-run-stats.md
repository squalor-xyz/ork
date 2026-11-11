---
{
  "id": 313,
  "title": "Where does ork show slicer's relay-run stats?",
  "status": "decided",
  "key": "stats.placement",
  "asked_by": "Jon",
  "decided_by": "Jon",
  "created": "2026-11-11",
  "decided_at": "2026-11-11",
  "context": "Question: Where does ork show slicer's relay-run stats?\nOptions:\n1. ork stats CLI: add `ork stats [PROJECT] [--since DATE] [--json]` and print what `slicer relay-run stats --json` returns. No daemon method, no goldens, N-class (no daemon or golden changes). The spec's Implement steps 4 and 5 and the Option a tests then apply as written.\n2. TUI panel: add a project-page panel with a new `stats.get` daemon method, a key, and goldens at 100x30 and 80x24. E-class (touches the daemon), bigger than size S, so the spec says to split it into wire and screen.\n3. doctor section: add a `stats` section and JSON key to `ork doctor`. Stats are information only and never fail doctor.\n4. combination: ship more than one of the above, which the spec treats as a split slice.\nRecommend: 1 - it is the smallest placement and the retrospective's use (R2, R4) is the owner reading numbers. Slice notes dated 2026-11-01 record that the drafter also recommends it. The spec itself says an implementer must not start a placement before the owner picks. `adj check` finds no record for this question under `stats.placement`, `relay.stats` or `ui.stats`, so it is not decided.\nDecision key: stats.placement",
  "decision": "ork stats CLI: add `ork stats [PROJECT] [--since DATE] [--json]` and print what `slicer relay-run stats --json` returns. No daemon method, no goldens, N-class (no daemon or golden changes). The spec's Implement steps 4 and 5 and the Option a tests then apply as written.",
  "rationale": "Answered in ork on 2026-11-11: Where does ork show slicer's relay-run stats?",
  "refs": [
    "S123"
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
