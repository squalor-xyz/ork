---
{
  "id": 373,
  "title": "How does ork run Grok in a write role?",
  "status": "decided",
  "key": "agent.grok-write-mode",
  "asked_by": "Jon",
  "decided_by": "Jon",
  "created": "2026-11-16",
  "decided_at": "2026-11-16",
  "context": "S305. The owner lifted the S72 headless-write ban for all grok on 2026-11-07 (interview); the grok accounts end 2026-12-09.",
  "options": [
    {
      "label": "dontask-allow",
      "summary": "--permission-mode dontAsk with --allow rules from the role's allowlist (write, search_replace, Bash rules); containment as Codex (decision 100): tree fingerprint, checks, review, gate"
    },
    {
      "label": "wait-bubblewrap",
      "summary": "install bubblewrap first so ork adds --sandbox workspace"
    }
  ],
  "recommendation": {
    "label": "dontask-allow",
    "reasoning": "probe 2026-11-07 (S305 notes): dontAsk with --allow write and search_replace edits headless; grok is not contained (wrote outside the repo; dontAsk ran touch), the same stance as Codex"
  },
  "decision": "dontask-allow",
  "rationale": "Owner chose 'dontAsk + allow rules (Recommended)' in an interactive question, relay session 8d711f67, 2026-11-16, to use grok as implementer before the accounts end on 2026-12-09; lifts the S72 ban as decided 2026-11-07.",
  "refs": [
    "S305"
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
