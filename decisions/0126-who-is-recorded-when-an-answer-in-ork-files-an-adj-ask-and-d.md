---
{
  "id": 126,
  "title": "Who is recorded when an answer in ork files an adj ask and decide?",
  "status": "decided",
  "key": "owner.questions-filing",
  "asked_by": "claude",
  "decided_by": "jon",
  "created": "2026-10-20",
  "decided_at": "2026-10-20",
  "context": "adj decide requires --why; --by defaults to the environment, which the daemon lacks.",
  "options": [
    {
      "label": "caller-env",
      "summary": "The TUI or CLI passes its own ADJ_DECIDED_BY in the request; if it is unset the answer is refused with a message; the reason text is 'Answered in ork on DATE' plus the question"
    }
  ],
  "recommendation": {
    "label": "caller-env",
    "reasoning": "Owner accepted the recommendation 2026-10-20."
  },
  "decision": "caller-env",
  "rationale": "Owner 2026-10-20: ork never invents who the owner is. The detached daemon has no ADJ_DECIDED_BY of its own.",
  "refs": [
    "S28"
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
