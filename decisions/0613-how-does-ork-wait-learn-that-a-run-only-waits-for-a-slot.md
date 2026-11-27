---
{
  "id": 613,
  "title": "How does ork wait learn that a run only waits for a slot?",
  "status": "decided",
  "key": "wait.resume-waiting-source",
  "asked_by": "Jon",
  "decided_by": "Jon",
  "created": "2026-11-27",
  "decided_at": "2026-11-27",
  "context": "S621 spec check.",
  "options": [
    {
      "label": "status-call",
      "summary": "read EngineRun.ResumeWaiting from the status call ork wait already makes; judge treats the run as in progress; no wire change"
    },
    {
      "label": "showreply-field",
      "summary": "add a field to daemon.ShowReply"
    }
  ],
  "recommendation": {
    "label": "status-call",
    "reasoning": "Recommended in the interactive question."
  },
  "decision": "status-call",
  "rationale": "Owner chose 'From the status call' in an interactive question, relay session 8d711f67, 2026-11-27.",
  "refs": [
    "S621"
  ],
  "history": [
    {
      "date": "2026-11-27",
      "event": "ask"
    },
    {
      "date": "2026-11-27",
      "event": "decide"
    }
  ]
}
---
