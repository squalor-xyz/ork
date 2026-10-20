---
{
  "id": 94,
  "title": "Does the default implementer allowlist cover read-only git -C, go -C and adj?",
  "status": "decided",
  "key": "engine.default-allowlist",
  "asked_by": "claude",
  "decided_by": "jon",
  "created": "2026-10-20",
  "decided_at": "2026-10-20",
  "context": "perm.go:24-33 blocks git -C, go -C and adj; the ork registry entry already adds them.",
  "options": [
    {
      "label": "yes",
      "summary": "add read-only forms to the default"
    },
    {
      "label": "no",
      "summary": "projects use registry permissions"
    }
  ],
  "recommendation": {
    "label": "yes"
  },
  "decision": "yes",
  "rationale": "Owner 2026-10-20: add the read-only git -C, go -C and adj forms to the default allowlist.",
  "refs": [
    "S23"
  ],
  "history": [
    {
      "date": "2026-10-20",
      "event": "ask"
    },
    {
      "date": "2026-10-20",
      "event": "decide"
    }
  ]
}
---
