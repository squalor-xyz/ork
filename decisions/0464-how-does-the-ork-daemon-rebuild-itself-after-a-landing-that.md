---
{
  "id": 464,
  "title": "How does the ork daemon rebuild itself after a landing that changes ork's own code?",
  "status": "decided",
  "key": "selfhost.auto-rebuild",
  "asked_by": "Jon",
  "decided_by": "Jon",
  "created": "2026-11-18",
  "decided_at": "2026-11-18",
  "context": "2026-11-18: ork daemon restart --rebuild refuses while any role runs (adj 312), so with three runs in flight the orchestrator had to retry it in a shell loop. adj 232 only names the rebuild in the landed line.",
  "options": [
    {
      "label": "drain",
      "summary": "opt-in per project; after a code landing the daemon holds new role starts, lets running roles finish, builds to a temp path, validates, swaps and restarts; runs resume; a failed build keeps the old daemon and releases the hold"
    },
    {
      "label": "idle",
      "summary": "opt-in per project; rebuild whenever no role runs, with no hold"
    },
    {
      "label": "adopt",
      "summary": "the new daemon adopts running role process groups, so no wait"
    },
    {
      "label": "manual",
      "summary": "keep adj 232"
    }
  ],
  "recommendation": {
    "label": "drain",
    "reasoning": "Bounded wait of one role length, kills nothing, and reuses the restart recovery ork already has."
  },
  "decision": "drain",
  "rationale": "Owner chose 'Drain then rebuild (Recommended)' in an interactive question, relay session 8d711f67, 2026-11-18.",
  "supersedes": [
    232
  ],
  "history": [
    {
      "date": "2026-11-18",
      "event": "ask"
    },
    {
      "date": "2026-11-18",
      "event": "decide"
    },
    {
      "date": "2026-11-18",
      "event": "supersede",
      "note": "supersedes 232"
    }
  ]
}
---
