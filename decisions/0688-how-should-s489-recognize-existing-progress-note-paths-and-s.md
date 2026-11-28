---
{
  "id": 688,
  "title": "How should S489 recognize existing progress-note paths and semicolon-delimited paths?",
  "status": "decided",
  "key": "brief.write-target-extraction",
  "asked_by": "Jon",
  "decided_by": "Jon",
  "created": "2026-11-27",
  "decided_at": "2026-11-27",
  "context": "The specified extractor cannot satisfy the ProgressFile assertions on existing briefs; its punctuation rule also rejects a valid UI temp directory.\nQuestion: How should S489 recognize existing progress-note paths and semicolon-delimited paths?\nOptions:\n1. Extend test-only extraction: recognize the Local progress note target before its later write cues and treat semicolons as path delimiters; retain all required-target assertions.\n2. Revise production briefs: authorize explicit write-to progress wording and unambiguous temp-path punctuation, revisiting the test-only constraint.\nRecommend: 1 - preserves the test-only decision and complete matrix checks without widening allowed paths.\nDecision key: brief.write-target-extraction",
  "decision": "Extend test-only extraction: recognize the Local progress note target before its later write cues and treat semicolons as path delimiters; keep every required-target assertion; production briefs stay unchanged (Jon chose 'Extend test-only extraction', session 8d711f67, 2026-11-28)",
  "rationale": "Answered in ork on 2026-11-28: How should S489 recognize existing progress-note paths and semicolon-delimited paths?",
  "refs": [
    "S489"
  ],
  "history": [
    {
      "date": "2026-11-27",
      "event": "ask"
    },
    {
      "date": "2026-11-27",
      "event": "decide"
    }
  ]
}
---
