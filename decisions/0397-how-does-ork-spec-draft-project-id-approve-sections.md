---
{
  "id": 397,
  "title": "How does ork spec draft PROJECT ID approve sections?",
  "status": "decided",
  "key": "cli.spec-draft-approval",
  "asked_by": "Jon",
  "decided_by": "Jon",
  "created": "2026-11-16",
  "decided_at": "2026-11-16",
  "context": "S179 spec check question; first answered by the orchestrator's mistaken piped input, then confirmed by the owner.",
  "options": [
    {
      "label": "prompt",
      "summary": "the CLI calls spec.draft, prints each proposed section (current, then new), asks y/n on stdin for each, calls spec.draft-apply for the approved ones only, and refuses without a terminal"
    },
    {
      "label": "file",
      "summary": "write proposals to a file and apply approved ones with a second command"
    }
  ],
  "recommendation": {
    "label": "prompt"
  },
  "decision": "prompt",
  "rationale": "Owner kept 'Prompt per section' in an interactive question, relay session 8d711f67, 2026-11-16, after the orchestrator reported its mistaken piped answer.",
  "refs": [
    "S179"
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
