---
{
  "id": 384,
  "title": "What does ork projects unpin NAME do once an adj pin exists?",
  "status": "decided",
  "key": "projects.unpin-default",
  "asked_by": "Jon",
  "decided_by": "Jon",
  "created": "2026-11-16",
  "decided_at": "2026-11-16",
  "context": "S206 spec check question.",
  "options": [
    {
      "label": "slicer-only",
      "summary": "unpin NAME clears only the slicer pin; --adj clears only the adj pin; --relay --adj clears both"
    },
    {
      "label": "both",
      "summary": "unpin NAME clears the slicer pin and the adj pin together"
    }
  ],
  "recommendation": {
    "label": "slicer-only",
    "reasoning": "today's command keeps its meaning"
  },
  "decision": "both",
  "rationale": "Owner chose 'Both' (not the recommendation) in an interactive question, relay session 8d711f67, 2026-11-16.",
  "refs": [
    "S206"
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
