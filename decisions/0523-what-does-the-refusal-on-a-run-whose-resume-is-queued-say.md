---
{
  "id": 523,
  "title": "What does the refusal on a run whose resume is queued say?",
  "status": "decided",
  "key": "ui.resume-waiting-refusal",
  "asked_by": "Jon",
  "decided_by": "Jon",
  "created": "2026-11-20",
  "decided_at": "2026-11-20",
  "context": "S449 UI pass: the refusal wrapped at 80x24 with a real run id.",
  "options": [
    {
      "label": "shorten-to-key",
      "summary": "resume queued; x stops it"
    },
    {
      "label": "shorten-keep-cli",
      "summary": "resume queued; ork stop RUN cancels it"
    },
    {
      "label": "accept-wrap",
      "summary": "Keep the wrap and amend the Check"
    }
  ],
  "recommendation": {
    "label": "shorten-to-key",
    "reasoning": "Recommended in the interactive question."
  },
  "decision": "shorten-to-key",
  "rationale": "Owner chose ''resume queued; x stops it' (Recommended)' in an interactive question, relay session 8d711f67, 2026-11-20.",
  "refs": [
    "S449"
  ],
  "history": [
    {
      "date": "2026-11-20",
      "event": "ask"
    },
    {
      "date": "2026-11-20",
      "event": "decide"
    }
  ]
}
---
