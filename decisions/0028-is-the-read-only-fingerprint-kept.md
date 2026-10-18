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
