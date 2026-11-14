---
{
  "id": 346,
  "title": "S356: how should local approval cover spec checks and assigned role paths?",
  "status": "decided",
  "key": "roles.local-approval-integration",
  "asked_by": "claude",
  "decided_by": "Jon",
  "recorded_by": "claude",
  "via": "interactive question, ork relay session 8d711f67",
  "created": "2026-11-14",
  "decided_at": "2026-11-14",
  "context": "Implementer question in run ork-20261113-011329-81c8: local spec checks have no RunID (internal/engine/speccheck.go:281); assigned temp/report paths fall outside the path rule (internal/engine/round.go:381-382).",
  "options": [
    {
      "label": "extend-both",
      "summary": "permission questions tied to the spec-check sitting, and the assigned temp directory and exact report file allowed through the path rule; allowlist kept"
    },
    {
      "label": "spec-only",
      "summary": "spec-check permission questions; assigned temp/report paths stay denied"
    },
    {
      "label": "paths-only",
      "summary": "assigned role paths allowed; uncovered spec-check calls denied at once"
    },
    {
      "label": "keep-current",
      "summary": "keep both denials and document the limits"
    }
  ],
  "recommendation": {
    "label": "extend-both",
    "reasoning": "Keeps the full approval point and the paths existing briefs assign."
  },
  "decision": "extend-both",
  "rationale": "Owner picked extend-both in an interactive question (ork relay session 8d711f67), answering S356 run ork-20261113-011329-81c8; recorded as the owner's delegate (process.owner-answers, 316).",
  "refs": [
    "S356"
  ],
  "history": [
    {
      "date": "2026-11-14",
      "event": "ask"
    },
    {
      "date": "2026-11-14",
      "event": "decide"
    }
  ]
}
---
