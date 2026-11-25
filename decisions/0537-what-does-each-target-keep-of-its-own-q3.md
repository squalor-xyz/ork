---
{
  "id": 537,
  "title": "What does each target keep of its own (Q3)?",
  "status": "decided",
  "key": "matrix.copy-keep",
  "asked_by": "Jon",
  "decided_by": "Jon",
  "created": "2026-11-24",
  "decided_at": "2026-11-24",
  "context": "Question: What does each target keep of its own (Q3)?\nOptions:\n1. keep own: each target keeps its spec-check row and rule and its UI-pass row and rule; the source's are not copied; a target with no spec-check rule is refused with `no_spec_check_row`; a copied rule that names a skipped row (ork's u11 names r14) is refused\n2. copy all: copy the source's spec-check and UI-pass rows and rules too, with ids rewritten (fully uniform, but ork's paths and skills reach every project)\n3. keep own, else author: as 1, but a target without a spec-check rule gets the source's row and a short rule that ork writes (ork then authors matrix text)\nRecommend: 1 - spec-check and UI-pass rules name project paths, and ork refuses a matrix without the spec-check rule (`internal/daemon/speccheck.go:262`)\nDecision key: matrix.copy-keep\nNote for the owner: the spec reads only `SpecCheckRowID` and `UIPassRowID`. The code now also has `SpecCheckRowIDs`, `UIPassRowIDs`, `LocalSpecCheckRowIDs` and `LocalUIPassRowIDs`, with rules such as \"Local spec check row (rNN)\" (`internal/slicercli/write.go:483-544`). Your answer must say whether Local spec-check and Local UI-pass rows and rules are kept like the others, and the planner and tests must cover every such row and rule, not one of each. The spec's \"kept rules first\" rule order (Implement step 4) is no longer forced, because slicer s444 is done and derives rule order itself (s444 notes, 2026-11-07). So the rule order is part of this answer.",
  "decision": "keep own: each target keeps its spec-check row and rule and its UI-pass row and rule; the source's are not copied; a target with no spec-check rule is refused with `no_spec_check_row`; a copied rule that names a skipped row (ork's u11 names r14) is refused",
  "rationale": "Answered in ork on 2026-11-25: What does each target keep of its own (Q3)? (S261)",
  "refs": [
    "S261"
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
