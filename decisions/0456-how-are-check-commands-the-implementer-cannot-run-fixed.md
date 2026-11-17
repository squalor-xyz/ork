---
{
  "id": 456,
  "title": "How are Check commands the implementer cannot run fixed?",
  "status": "decided",
  "key": "checks.implementer-runnable",
  "asked_by": "Jon",
  "decided_by": "Jon",
  "created": "2026-11-17",
  "decided_at": "2026-11-17",
  "context": "checkCommandRunnable (internal/engine/brief.go:547-550) treats a quoted | as a pipe, so go test -run 'A|B' is orchestrator-only; rg is not allowlisted. Hit S402, S396, S397, S390, S332, S388.",
  "options": [
    {
      "label": "fix-interim",
      "summary": "Fix ork in code; answer slices 'orchestrator runs them' meanwhile"
    },
    {
      "label": "rewrite",
      "summary": "Rewrite each spec's lines to runnable forms"
    },
    {
      "label": "fix-first",
      "summary": "Fix ork in code first and hold the affected slices until it lands"
    }
  ],
  "recommendation": {
    "label": "fix-interim",
    "reasoning": "deterministic fix in code; the question recurs (adj 249, 255, 256, 259)"
  },
  "decision": "fix-first",
  "rationale": "Owner chose 'Fix first, then run' in an interactive question, relay session 8d711f67, 2026-11-17.",
  "history": [
    {
      "date": "2026-11-17",
      "event": "ask"
    },
    {
      "date": "2026-11-17",
      "event": "decide"
    }
  ]
}
---
