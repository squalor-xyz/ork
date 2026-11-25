---
{
  "id": 562,
  "title": "What rule decides that an open item is a likely duplicate of the slice in the Direction section?",
  "status": "decided",
  "key": "feedback.direction-duplicates",
  "asked_by": "Jon",
  "decided_by": "Jon",
  "created": "2026-11-25",
  "decided_at": "2026-11-25",
  "context": "Question: What rule decides that an open item is a likely duplicate of the slice in the Direction section?\nOptions:\n1. title-jaccard: reuse `NearDuplicateTitles` (`internal/engine/neardup.go:15`, S442, adj 493) at 0.5 on the slice's own title against each open item's title. This is the smallest change, but it misses reworded pairs. The S550/S552 evidence pair scores about 0.35, below 0.5.\n2. why-containment: count the open item's title words (the `titleWords` set) found in the slice's Why text. List the item when the share is at least a threshold you name, for example 0.6. This matches \"Why text matches a title\" and can catch the S550/S552 case. It needs a new helper and a threshold. The empty Files, Failing tests, Implement and Check sections are then filled per your 2026-11-25 `tighten` note.\n3. as written: leave the spec open until you give a rule in the slice text. Files, Failing tests, Implement and Check are empty today, and `directionDigest` (`internal/engine/brief.go:458`) has no duplicate step.\nRecommend: 2 - the slice title says the Why text matches the item title, and title-only matching misses the evidence pair.\nDecision key: feedback.direction-duplicates",
  "decision": "why-containment: count the open item's title words (the `titleWords` set) found in the slice's Why text. List the item when the share is at least a threshold you name, for example 0.6. This matches \"Why text matches a title\" and can catch the S550/S552 case. It needs a new helper and a threshold. The empty Files, Failing tests, Implement and Check sections are then filled per your 2026-11-25 `tighten` note.",
  "rationale": "Answered in ork on 2026-11-25: What rule decides that an open item is a likely duplicate of the slice in the Direction section? (S557)",
  "refs": [
    "S557"
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
