---
{
  "id": 93,
  "title": "What does ork do when a finished role had denied tool calls?",
  "status": "decided",
  "key": "engine.denials",
  "asked_by": "claude",
  "decided_by": "jon",
  "created": "2026-10-20",
  "decided_at": "2026-10-20",
  "context": "S21 stopped with 'implementer round 1 exited error' after the role finished; three stray side calls were denied. classify.go:20 marks any denial as denied; runner.go:35-41 maps it to ExitError.",
  "options": [
    {
      "label": "warn",
      "summary": "record denied tools on the run, show them, and proceed when the round has a success result"
    },
    {
      "label": "stop",
      "summary": "any denial stops the slice"
    },
    {
      "label": "by-tool",
      "summary": "configured tools stop, others warn"
    }
  ],
  "recommendation": {
    "label": "warn"
  },
  "decision": "warn",
  "rationale": "Owner 2026-10-20: warn and continue; a denial with no success result still stops.",
  "refs": [
    "S23"
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
