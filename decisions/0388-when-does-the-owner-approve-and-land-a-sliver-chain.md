---
{
  "id": 388,
  "title": "When does the owner approve and land a sliver chain?",
  "status": "decided",
  "key": "slice.sliver-chain-approval",
  "asked_by": "Jon",
  "decided_by": "Jon",
  "created": "2026-11-16",
  "decided_at": "2026-11-16",
  "context": "S62 spec check question.",
  "options": [
    {
      "label": "once-at-end",
      "summary": "each sliver runs implement, review and gate with no owner stop; the owner approves once after the last sliver and ork lands the parent's branch once"
    },
    {
      "label": "each-sliver",
      "summary": "pause for owner approval after every sliver's gate; land once at the end"
    }
  ],
  "recommendation": {
    "label": "once-at-end",
    "reasoning": "one worktree, one landing (spec; slicer landing.md:42-47)"
  },
  "decision": "once-at-end",
  "rationale": "Owner chose 'Once at the end (Recommended)' in an interactive question, relay session 8d711f67, 2026-11-16.",
  "refs": [
    "S62"
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
