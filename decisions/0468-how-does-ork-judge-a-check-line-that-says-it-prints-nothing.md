---
{
  "id": 468,
  "title": "How does ork judge a Check line that says it prints nothing?",
  "status": "decided",
  "key": "checks.prints-nothing-exit",
  "asked_by": "Jon",
  "decided_by": "Jon",
  "created": "2026-11-18",
  "decided_at": "2026-11-18",
  "context": "2026-11-18: S388, S398 and S392 went red because an absence check such as rg -n PATTERN PATH exits 1 when it finds nothing, and ork requires exit 0 (internal/engine/states.go, c.OK).",
  "options": [
    {
      "label": "no-match-exit",
      "summary": "pass on empty output with exit 0, or exit 1 from rg, grep or git grep; exit 2+, any output, or exit 1 from another command fails; a piped line is judged by its last command"
    },
    {
      "label": "empty-any-exit",
      "summary": "pass whenever the output is empty, whatever the exit"
    },
    {
      "label": "strict",
      "summary": "keep exit 0 and have spec authors write absence checks that exit 0"
    }
  ],
  "recommendation": {
    "label": "no-match-exit",
    "reasoning": "rg and grep already say found or not found by exit code; other failures still fail."
  },
  "decision": "no-match-exit",
  "rationale": "Owner chose 'Exit 1 + no output passes (Recommended)' in an interactive question, relay session 8d711f67, 2026-11-18.",
  "refs": [
    "S419"
  ],
  "history": [
    {
      "date": "2026-11-18",
      "event": "ask"
    },
    {
      "date": "2026-11-18",
      "event": "decide"
    }
  ]
}
---
