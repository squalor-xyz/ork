---
{
  "id": 327,
  "title": "Should the run-question answer path and the ledger answer path also use the owner setting?",
  "status": "decided",
  "key": "owner.filing-run-answers",
  "asked_by": "claude",
  "decided_by": "Jon",
  "via": "interactive question, relay session 8d711f67, 2026-11-12",
  "created": "2026-11-12",
  "decided_at": "2026-11-12",
  "context": "S314 run ork-20261111-233334-159b question, 2026-11-12.",
  "options": [
    {
      "label": "spec-only",
      "summary": "keep S314 as built; run answers (ctrl+a, FileAnswer) and ledger answers still need ADJ_DECIDED_BY"
    },
    {
      "label": "run-answers",
      "summary": "the daemon resolves the owner for run.answer and filing is on by default when an owner is known, with ctrl+a as the per-session off switch"
    },
    {
      "label": "run-and-ledger",
      "summary": "as run-answers, plus relayanswer (TUI and daemon) stops refusing when only the setting names the owner"
    }
  ],
  "recommendation": {
    "label": "run-answers",
    "reasoning": "Matches S314's Implement step 3 and the decision log as the owner's record; it changes the TUI pane and runs.go, so it is its own slice."
  },
  "decision": "run-answers",
  "rationale": "Owner picked 'Extend to run answers, new slice (Recommended)': S314 stays as built and a follow-up slice extends filing to run answers; recorded by the agent as the owner's delegate (adj 316).",
  "refs": [
    "S314"
  ],
  "history": [
    {
      "date": "2026-11-12",
      "event": "ask"
    },
    {
      "date": "2026-11-12",
      "event": "decide"
    }
  ]
}
---
