---
{
  "id": 128,
  "title": "How does ork detect a merged branch, and when may it run?",
  "status": "decided",
  "key": "landing.merge-detect",
  "asked_by": "claude",
  "decided_by": "jon",
  "created": "2026-10-20",
  "decided_at": "2026-10-20",
  "context": "Record 66 allows only git rev-parse HEAD as the change signal; S09 and S63 need to know which committed branches are merged.",
  "options": [
    {
      "label": "on-head-change",
      "summary": "When a project's HEAD changes (the signal of record 66), run one merged-branch check for that project's committed runs; no extra git reads while HEAD is unchanged"
    }
  ],
  "recommendation": {
    "label": "on-head-change",
    "reasoning": "Owner accepted the recommendation 2026-10-20."
  },
  "decision": "on-head-change",
  "rationale": "Owner 2026-10-20: accepted with the note that this adds one git command beyond record 66's head-only signal.",
  "refs": [
    "S63,S09"
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
