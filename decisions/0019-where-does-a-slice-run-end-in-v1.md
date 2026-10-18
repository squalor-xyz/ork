---
{
  "id": 19,
  "title": "Where does a slice run end in v1?",
  "status": "decided",
  "key": "wave.end",
  "asked_by": "claude",
  "decided_by": "jon",
  "created": "2026-10-18",
  "decided_at": "2026-10-18",
  "context": "Landing has merge-conflict work (changelog).",
  "options": [
    {
      "label": "commit",
      "summary": "Commit on the slice branch"
    },
    {
      "label": "landed",
      "summary": "Merge and land in v1"
    },
    {
      "label": "gate",
      "summary": "Stop at a passed gate, uncommitted"
    }
  ],
  "decision": "commit",
  "rationale": "Owner interview 2026-10-18: commit on the slice branch after PASS and owner approval; landing stays manual.",
  "history": [
    {
      "date": "2026-10-18",
      "event": "ask"
    },
    {
      "date": "2026-10-18",
      "event": "decide"
    }
  ]
}
---
