---
{
  "id": 554,
  "title": "Where does the source row content ride, and is it content or a hash?",
  "status": "decided",
  "key": "matrix.copy-bind-content",
  "asked_by": "Jon",
  "decided_by": "Jon",
  "created": "2026-11-25",
  "decided_at": "2026-11-25",
  "context": "Question: Where does the source row content ride, and is it content or a hash?\nOptions:\n1. digest: A per-project SHA-256 of the planned rows goes in a new MatrixCopyProject field. The CLI sends it back in a new MatrixCopyParams.ExpectRows map, and checkCopy refuses a mismatch with CodeStalePreview. MatrixCopyStep stays slicer's output shape.\n2. rows: The full row content goes in the preview result and comes back in a params map. The daemon compares it field for field.\n3. step field: A digest field is added to MatrixCopyStep and compared with slices.Equal. This mixes ork data into slicer's step type.\nRecommend: 1 - it matches the owner's note (\"content or a hash\"), adds little to the wire and keeps MatrixCopyStep as slicer's shape.\nDecision key: matrix.copy-bind-content",
  "decision": "digest: A per-project SHA-256 of the planned rows goes in a new MatrixCopyProject field. The CLI sends it back in a new MatrixCopyParams.ExpectRows map, and checkCopy refuses a mismatch with CodeStalePreview. MatrixCopyStep stays slicer's output shape.",
  "rationale": "Answered in ork on 2026-11-25: Where does the source row content ride, and is it content or a hash? (S558)",
  "refs": [
    "S558"
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
