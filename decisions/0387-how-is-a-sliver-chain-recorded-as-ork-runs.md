---
{
  "id": 387,
  "title": "How is a sliver chain recorded as ork runs?",
  "status": "decided",
  "key": "slice.sliver-chain-record",
  "asked_by": "Jon",
  "decided_by": "Jon",
  "created": "2026-11-16",
  "decided_at": "2026-11-16",
  "context": "S62 spec check question.",
  "options": [
    {
      "label": "per-sliver",
      "summary": "each sliver is an ordinary run record in the parent's worktree, linked by a chain id; the chain holds one cap slot until its last sliver ends; a lone sliver has no chain"
    },
    {
      "label": "chain-record",
      "summary": "one record with Item = parent and an ordered sliver list with per-sliver state"
    }
  ],
  "recommendation": {
    "label": "per-sliver",
    "reasoning": "the lone-sliver path already needs a sliver run record; stop and retry keep their behaviour"
  },
  "decision": "per-sliver",
  "rationale": "Owner chose 'One record each (Recommended)' in an interactive question, relay session 8d711f67, 2026-11-16.",
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
