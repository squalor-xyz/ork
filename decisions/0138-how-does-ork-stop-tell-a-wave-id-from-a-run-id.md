---
{
  "id": 138,
  "title": "How does ork stop tell a wave id from a run id?",
  "status": "open",
  "key": "wave.stop-target",
  "asked_by": "claude",
  "created": "2026-10-20",
  "context": "S60 open (a) and (b). S05 already builds wave ids as wave-YYYYMMDD-HHMMSS-xxxx (internal/daemon/runs.go:320-324). S60 adds ork stop WAVE beside ork stop RUN.",
  "options": [
    {
      "label": "prefix",
      "summary": "Keep the wave- id format; ork stop WAVE is told apart by the wave- prefix, one command",
      "tradeoffs": "One command, but depends on run ids never starting with wave-"
    },
    {
      "label": "separate",
      "summary": "A separate command, ork wave stop WAVE; ork stop takes run ids only",
      "tradeoffs": "No ambiguity, one more command to learn and document"
    }
  ],
  "recommendation": {
    "label": "prefix",
    "reasoning": "The id format already exists and the design sketch has ork stop WAVE."
  },
  "refs": [
    "S60"
  ],
  "history": [
    {
      "date": "2026-10-20",
      "event": "ask"
    }
  ]
}
---
