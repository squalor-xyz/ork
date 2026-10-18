---
{
  "id": 21,
  "title": "What happens to a wave when one slice fails or stops?",
  "status": "decided",
  "key": "wave.failure",
  "asked_by": "claude",
  "decided_by": "jon",
  "created": "2026-10-18",
  "decided_at": "2026-10-18",
  "context": "Stop rules: gate FAIL 3x, ambiguous spec, owner question.",
  "options": [
    {
      "label": "continue",
      "summary": "Others continue"
    },
    {
      "label": "pause",
      "summary": "Pause queued starts"
    },
    {
      "label": "stop",
      "summary": "Stop the whole wave"
    }
  ],
  "decision": "continue",
  "rationale": "Owner interview 2026-10-18: others continue; the failed slice waits in needs-you.",
  "history": [
    {
      "date": "2026-10-18",
      "event": "ask"
    },
    {
      "date": "2026-10-18",
      "event": "decide"
    }
  ]
}
---
