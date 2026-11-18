---
{
  "id": 461,
  "title": "Which branch does ork tracking commit treat as the base branch?",
  "status": "decided",
  "key": "cli.tracking-commit-base",
  "asked_by": "Jon",
  "decided_by": "Jon",
  "created": "2026-11-18",
  "decided_at": "2026-11-18",
  "context": "S405 spec check (2026-11-18): ork tracking commit commits tracking files on the base branch; the spec's refusal text names main.",
  "options": [
    {
      "label": "flag-default-main",
      "summary": "--base BRANCH flag, default main; the daemon passes it to CommitTrackingFiles; nothing stored"
    },
    {
      "label": "registry-option",
      "summary": "a per-project base_branch registry option set with ork projects set"
    },
    {
      "label": "checked-out",
      "summary": "no fixed base; whatever branch the main checkout is on, refusing a detached HEAD or a live run's slice branch"
    }
  ],
  "recommendation": {
    "label": "flag-default-main",
    "reasoning": "Keeps the spec's refusal and adds no registry field or data model."
  },
  "decision": "flag-default-main",
  "rationale": "Owner chose '--base flag, default main (Recommended)' in an interactive question, relay session 8d711f67, 2026-11-18.",
  "refs": [
    "S405"
  ],
  "history": [
    {
      "date": "2026-11-18",
      "event": "ask"
    },
    {
      "date": "2026-11-18",
      "event": "decide"
    }
  ]
}
---
