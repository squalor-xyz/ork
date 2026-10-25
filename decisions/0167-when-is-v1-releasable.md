---
{
  "id": 167,
  "title": "When is v1 releasable?",
  "status": "decided",
  "key": "release.v1-exit",
  "asked_by": "claude",
  "decided_by": "jon",
  "created": "2026-10-25",
  "decided_at": "2026-10-25",
  "context": "Retrospective R7 (docs/review-2026-10-25-relay-retrospective.md). v1 has no exit line; nine slices were filed in one day, so the queue grows as it drains.",
  "options": [
    {
      "label": "ork-only-loop",
      "summary": "v1 is releasable when the owner can run a wave, answer its questions, approve it and land it, all from ork, with no outside agent session; polish found after that goes to v1.1 by default"
    },
    {
      "label": "queue-empty",
      "summary": "v1 is releasable when every v1 slice is done"
    }
  ],
  "recommendation": {
    "label": "ork-only-loop",
    "reasoning": "Ties the release to the product goal and stops polish from holding it."
  },
  "decision": "ork-only-loop",
  "rationale": "Owner approved the recommendation in the agent session on 2026-10-25 (AskUserQuestion: yes, decide all 162-170); recorded by the agent at the owner's direction. ",
  "history": [
    {
      "date": "2026-10-25",
      "event": "ask"
    },
    {
      "date": "2026-10-25",
      "event": "decide"
    }
  ]
}
---
