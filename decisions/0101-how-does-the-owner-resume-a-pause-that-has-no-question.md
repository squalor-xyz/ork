---
{
  "id": 101,
  "title": "How does the owner resume a pause that has no question?",
  "status": "decided",
  "key": "engine.pause-resume",
  "asked_by": "claude",
  "decided_by": "jon",
  "created": "2026-10-20",
  "decided_at": "2026-10-20",
  "context": "Seen 2026-10-20 (run ork-20261020-120934-e8ac): a report-shape pause could not be answered or retried. Engine.Retry exists (internal/engine/engine.go:318) but no daemon method or CLI calls it.",
  "options": [
    {
      "label": "retry",
      "summary": "ork retry RUN calls Engine.Retry; ork answer stays for questions only",
      "tradeoffs": "Smallest change; a rerun may repeat the same bad shape, but the owner can stop."
    },
    {
      "label": "answer-text",
      "summary": "ork answer RUN TEXT on any pause reruns the role with the text as feedback",
      "tradeoffs": "Needs a feedback path into the round prompt; larger."
    },
    {
      "label": "both",
      "summary": "ork retry plus corrective answer text",
      "tradeoffs": "Most capable and most work."
    },
    {
      "label": "shape-question",
      "summary": "a shape failure is recorded as a question with retry/stop options",
      "tradeoffs": "Overlaps S26 and changes the Pending kinds."
    }
  ],
  "recommendation": {
    "label": "retry",
    "reasoning": "The engine already has Retry; exposing it closes the gap with no new engine behavior."
  },
  "decision": "retry",
  "rationale": "Owner 2026-10-20: ork retry RUN, via the S44 relay; ork answer stays for real questions.",
  "refs": [
    "S44"
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
