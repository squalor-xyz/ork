---
{
  "id": 325,
  "title": "How much of relay v12's tool feedback does ork build?",
  "status": "decided",
  "key": "relay.tool-feedback",
  "asked_by": "claude",
  "decided_by": "Jon",
  "via": "interactive question, relay session 8d711f67, 2026-11-12",
  "created": "2026-11-12",
  "decided_at": "2026-11-12",
  "context": "Relay v12 (slicer 1.21.0, s453): every role may end its report with up to 3 tool feedback lines (friction, feature, deterministic-check). The slice is S354 (Accept relay v12); the S348 ref was entered before the slice was filed and is wrong. Its sibling contract-set record is adj 326.",
  "options": [
    {
      "label": "full",
      "summary": "role prompts allow up to 3 tool feedback lines; ork parses them from role reports and records them in the run, at most 3, advisory, never a finding"
    },
    {
      "label": "accept-only",
      "summary": "bump the version and prompts; ignore tool feedback"
    },
    {
      "label": "full-surface",
      "summary": "full, plus show entries in the TUI run pane and ork show"
    }
  ],
  "recommendation": {
    "label": "full",
    "reasoning": "Follows the contract without a UI pass in the same slice."
  },
  "decision": "full",
  "rationale": "Owner picked 'Full (Recommended)'; recorded by the agent as the owner's delegate (adj 316).",
  "refs": [
    "S348"
  ],
  "history": [
    {
      "date": "2026-11-12",
      "event": "ask"
    },
    {
      "date": "2026-11-12",
      "event": "decide"
    },
    {
      "date": "2026-11-12",
      "event": "amend",
      "note": "context",
      "was": {
        "context": "Relay v12 (slicer 1.21.0, s453): every role may end its report with up to 3 tool feedback lines (friction, feature, deterministic-check)."
      }
    },
    {
      "date": "2026-11-12",
      "event": "amend",
      "note": "context",
      "was": {
        "context": "Relay v12 (slicer 1.21.0, s453): every role may end its report with up to 3 tool feedback lines (friction, feature, deterministic-check). The slice is S354 (Accept relay v12); the S348 ref was entered before the slice was filed and is wrong."
      }
    }
  ]
}
---
