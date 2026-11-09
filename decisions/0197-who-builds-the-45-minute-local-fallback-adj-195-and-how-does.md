---
{
  "id": 197,
  "title": "Who builds the 45-minute local fallback (adj 195), and how does it relate to the 30-minute hung timer?",
  "status": "decided",
  "key": "roles.local-model.timeout-owner",
  "asked_by": "Jon",
  "decided_by": "Jon",
  "created": "2026-11-09",
  "decided_at": "2026-11-09",
  "context": "Question: Who builds the 45-minute local fallback (adj 195), and how does it relate to the 30-minute hung timer?\nOptions:\n1. S301: add a step and a failing test here: a `-local` role that runs past 45 minutes is stopped and falls back to the next row. Keep `engine.DefaultHungAfter` (30 minutes of silence) unchanged for cloud roles.\n2. S303: move it there, since adj 195 also refs S303; S301 only verifies that the 30-minute timer does not kill a slow local reply\n3. as written: S301 has no 45-minute step or test, and adj 195 is left without an owner slice\nRecommend: 2 - S303 already owns the fallback classification and its test, and S301's step 5 can stay a check.\nDecision key: roles.local-model.timeout-owner",
  "decision": "S301: add a step and a failing test here: a `-local` role that runs past 45 minutes is stopped and falls back to the next row. Keep `engine.DefaultHungAfter` (30 minutes of silence) unchanged for cloud roles.",
  "rationale": "Answered in ork on 2026-11-09: Who builds the 45-minute local fallback (adj 195), and how does it relate to the 30-minute hung timer?",
  "refs": [
    "S301"
  ],
  "history": [
    {
      "date": "2026-11-09",
      "event": "ask"
    },
    {
      "date": "2026-11-09",
      "event": "decide"
    }
  ]
}
---
