---
{
  "id": 296,
  "title": "S304: which local launchers and roles does the evaluation cover?",
  "status": "decided",
  "key": "roles.local-eval-set",
  "asked_by": "claude",
  "decided_by": "Jon",
  "via": "interactive question, relay session 8d711f67, 2026-11-11",
  "created": "2026-11-11",
  "decided_at": "2026-11-11",
  "context": "S304 spec check 2026-11-11. claude-local ran the S291 implementer for 45 min with no output on 2026-11-11; grok-local fails on a policy lock file; codex-local waits for bubblewrap.",
  "options": [
    {
      "label": "claude-local-only",
      "summary": "evaluate claude-local in each role against a cloud run of the same small slice; grok-local and codex-local wait"
    },
    {
      "label": "claude-and-grok-local",
      "summary": "also grok-local"
    },
    {
      "label": "all-three",
      "summary": "also codex-local"
    }
  ],
  "recommendation": {
    "label": "claude-local-only",
    "reasoning": "Only claude-local can start today."
  },
  "decision": "claude-local-only",
  "rationale": "Owner picked option 1 and added: in normal runs the reviewer, gate and spec check stay on cloud Claude and local is for implementers only (note it is slow); the S304 trial does try claude-local in the various roles to compare; at most 2 local roles at one time, global cap; the owner is using the local model now, so S304 waits for the owner's go-ahead.",
  "refs": [
    "S304"
  ],
  "history": [
    {
      "date": "2026-11-11",
      "event": "ask"
    },
    {
      "date": "2026-11-11",
      "event": "decide"
    }
  ]
}
---
