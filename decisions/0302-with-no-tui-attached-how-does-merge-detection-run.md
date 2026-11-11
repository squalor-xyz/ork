---
{
  "id": 302,
  "title": "With no TUI attached, how does merge detection run?",
  "status": "decided",
  "key": "poll.idle-merge-detection",
  "asked_by": "claude",
  "decided_by": "Jon",
  "via": "interactive question, relay session 8d711f67, 2026-11-11",
  "created": "2026-11-11",
  "decided_at": "2026-11-11",
  "options": [
    {
      "label": "attach-only",
      "summary": "No timed HEAD read with no TUI; checkMerged runs when a TUI attaches"
    },
    {
      "label": "head-timer",
      "summary": "Keep the HEAD part on a 30 s timer with no TUI"
    },
    {
      "label": "own-timer",
      "summary": "A daemon merge-detection timer outside the cache"
    }
  ],
  "recommendation": {
    "label": "attach-only",
    "reasoning": "follows adj 301; idle merge cleanup resumes on attach"
  },
  "decision": "attach-only",
  "rationale": "Owner picked wait for attach",
  "refs": [
    "S336"
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
