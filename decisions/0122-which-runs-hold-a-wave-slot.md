---
{
  "id": 122,
  "title": "Which runs hold a wave slot?",
  "status": "decided",
  "key": "wave.slots",
  "asked_by": "claude",
  "decided_by": "jon",
  "created": "2026-10-20",
  "decided_at": "2026-10-20",
  "context": "Record 113 puts the caps in one admission point; this fixes what counts against them.",
  "options": [
    {
      "label": "running-role-only",
      "summary": "Only a run with a role process running holds a slot; paused and awaiting-approval runs do not; approve, answer and retry bypass the cap"
    }
  ],
  "recommendation": {
    "label": "running-role-only",
    "reasoning": "Owner accepted the recommendation 2026-10-20."
  },
  "decision": "running-role-only",
  "rationale": "Owner 2026-10-20: waiting on the owner must not starve the queue. The cap can briefly exceed its value when several runs are approved at once.",
  "refs": [
    "S05"
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
