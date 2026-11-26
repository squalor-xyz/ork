---
{
  "id": 600,
  "title": "Which changed paths should the \"No renders expected\" rule ignore?",
  "status": "decided",
  "key": "ui.no-renders-paths",
  "asked_by": "Jon",
  "decided_by": "Jon",
  "created": "2026-11-25",
  "decided_at": "2026-11-25",
  "context": "The relay sees `.slicer/` and `landed-changed/` tracking files in every slice's diff, so the \"only internal/tui tests\" rule as specified never matches a real run, including S531, the case it was written for.\n\nQuestion: Which changed paths should the \"No renders expected\" rule ignore?\nOptions:\n1. ignore-tracking: skip `.slicer/` and `landed-changed/` paths, then require every remaining path to be a `_test.go` file under `internal/tui/`\n2. ignore-tracking-docs: option 1, and also skip `docs/` paths\n3. as-spec: keep the literal rule even though real runs never match it\nRecommend: 1 - it matches the S531 case and keeps the rule narrow\nDecision key: ui.no-renders-paths",
  "decision": "ignore-tracking: skip .slicer/ and landed-changed/ paths, then require at least one remaining path, all of them _test.go files under internal/tui/ (Jon chose 'Ignore tracking', session 8d711f67, 2026-11-26)",
  "rationale": "Answered in ork on 2026-11-26: Which changed paths should the \"No renders expected\" rule ignore?",
  "refs": [
    "S584"
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
