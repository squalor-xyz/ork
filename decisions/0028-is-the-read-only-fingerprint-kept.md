---
{
  "id": 28,
  "title": "Is the read-only fingerprint kept?",
  "status": "decided",
  "key": "role.readonly-fingerprint",
  "asked_by": "claude",
  "decided_by": "jon",
  "created": "2026-10-18",
  "decided_at": "2026-10-18",
  "context": "relay-call.sh hashes the tree around read-only roles.",
  "options": [
    {
      "label": "yes",
      "summary": "Fingerprint read-only roles"
    },
    {
      "label": "no",
      "summary": "Permissions only"
    }
  ],
  "decision": "yes",
  "rationale": "Owner interview 2026-10-18: keep it; a read-only role that edits files fails.",
  "narrowed_by": [
    {
      "id": 476,
      "scope": "spec checks leave the project's own .slicer/ out of the fingerprint"
    }
  ],
  "history": [
    {
      "date": "2026-10-18",
      "event": "ask"
    },
    {
      "date": "2026-10-18",
      "event": "decide"
    },
    {
      "date": "2026-11-18",
      "event": "narrow",
      "note": "by 476: spec checks leave the project's own .slicer/ out of the fingerprint"
    }
  ]
}
---
