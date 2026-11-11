---
{
  "id": 294,
  "title": "S289: which tracking-only landing conflicts does the pause cover?",
  "status": "decided",
  "key": "landing.tracking-conflict-scope",
  "asked_by": "claude",
  "decided_by": "Jon",
  "via": "interactive question, relay session 8d711f67, 2026-11-11",
  "created": "2026-11-11",
  "decided_at": "2026-11-11",
  "options": [
    {
      "label": "slice-json-only",
      "summary": "only .slicer/slices/*.json changed on both sides, merge-both wording (adj 292)"
    },
    {
      "label": "widen",
      "summary": "also deleted-by-us tracking files and generated render files"
    }
  ],
  "recommendation": {
    "label": "slice-json-only",
    "reasoning": "Render and index files are handled by the configured merge drivers."
  },
  "decision": "slice-json-only",
  "rationale": "Owner picked Slice JSON only (recommended).",
  "refs": [
    "S289"
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
