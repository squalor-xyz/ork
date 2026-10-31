---
{
  "id": 147,
  "title": "Does ork wave suggest, start and stop WAVE work on a real project?",
  "status": "decided",
  "key": "review.s60-wave",
  "asked_by": "claude",
  "decided_by": "Jon R",
  "created": "2026-10-21",
  "decided_at": "2026-10-31",
  "context": "Run wave suggest on a project with files_section set, then wave start on two slices, then stop WAVE. Also: text-mode errors print the message without the code (same as every command); a short race remains between stopWave's final-state check and ownerRun; no test covers the stopWave queue restart after a failed queued-member stop.",
  "options": [
    {
      "label": "ok",
      "summary": "Works as expected",
      "tradeoffs": "Closes the S60 human check."
    },
    {
      "label": "fix",
      "summary": "Needs changes; file a follow-up",
      "tradeoffs": "Costs a follow-up."
    }
  ],
  "recommendation": {
    "label": "ok",
    "reasoning": "Gate passed; tests cover each command."
  },
  "decision": "ok",
  "rationale": "Owner picked this option in an interactive question in the v1.0 release session on 2026-10-31 (AskUserQuestion), recommendation followed.",
  "refs": [
    "S60"
  ],
  "history": [
    {
      "date": "2026-10-21",
      "event": "ask"
    },
    {
      "date": "2026-10-31",
      "event": "decide"
    }
  ]
}
---
