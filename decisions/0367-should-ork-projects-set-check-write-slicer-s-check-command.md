---
{
  "id": 367,
  "title": "Should ork projects set --check write slicer's check_command?",
  "status": "decided",
  "key": "project.check-command-write",
  "asked_by": "Jon",
  "decided_by": "Jon",
  "created": "2026-11-16",
  "decided_at": "2026-11-16",
  "context": "S246 question (run ork-20261116-012801-28aa).",
  "options": [
    {
      "label": "registry",
      "summary": "keep writing the registry; edit slicer's key with slicer config"
    },
    {
      "label": "slicer",
      "summary": "write slicer's check_command and leave the registry unchanged"
    },
    {
      "label": "both",
      "summary": "write the same command to both stores"
    }
  ],
  "recommendation": {
    "label": "registry",
    "reasoning": "adj 32 decides the read order; this slice keeps the registry write"
  },
  "decision": "registry",
  "rationale": "Owner chose 'Registry (Recommended)' in an interactive question, relay session 8d711f67, 2026-11-16.",
  "refs": [
    "S246"
  ],
  "history": [
    {
      "date": "2026-11-16",
      "event": "ask"
    },
    {
      "date": "2026-11-16",
      "event": "decide"
    }
  ]
}
---
