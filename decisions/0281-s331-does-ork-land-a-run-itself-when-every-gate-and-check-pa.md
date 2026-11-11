---
{
  "id": 281,
  "title": "S331: does ork land a run itself when every gate and check passes?",
  "status": "decided",
  "key": "landing.auto-approve",
  "asked_by": "claude",
  "decided_by": "Jon",
  "via": "interactive question, relay session 8d711f67, 2026-11-11",
  "created": "2026-11-11",
  "decided_at": "2026-11-11",
  "context": "Owner 2026-11-11: 'if a slice passes all of its gates and checks, it is approved and should be landed and cleaned up. standard process. no need to wait for human checks. human checks can go into another TODO type file human-checks-todo.md (i may or may not do them, usually i test after release, and we fix if there are issues)'. adj 162 (landing.in-ork) kept the owner's approve step.",
  "options": [
    {
      "label": "default-for-all",
      "summary": "gate VERDICT: PASS and checks green means approved; ork lands and cleans up with no approve call, for every project; a per-project --auto-land off is only an opt-out; human checks never block and are appended to human-checks-todo.md at the repo root"
    },
    {
      "label": "per-project",
      "summary": "a setting off by default, turned on per project"
    }
  ],
  "recommendation": {
    "label": "default-for-all",
    "reasoning": "Owner's words: standard process."
  },
  "decision": "default-for-all",
  "rationale": "Owner stated the rule in the session and picked 'Default for all (Recommended)' and 'Repo root, backfill (Recommended)' in an interactive question on 2026-11-11.",
  "refs": [
    "S331",
    "S162"
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
