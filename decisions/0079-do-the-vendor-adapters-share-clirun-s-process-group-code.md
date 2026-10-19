---
{
  "id": 79,
  "title": "Do the vendor adapters share clirun's process-group code?",
  "status": "decided",
  "key": "code.process-groups",
  "asked_by": "claude",
  "decided_by": "jon",
  "created": "2026-10-19",
  "decided_at": "2026-10-19",
  "context": "clirun's helpers are unexported and its Run buffers output; adapters must stream.",
  "options": [
    {
      "label": "export",
      "summary": "Export from clirun"
    },
    {
      "label": "copy",
      "summary": "Keep a copy in each adapter"
    }
  ],
  "decision": "export",
  "rationale": "Owner 2026-10-19: export the start and kill-group helpers from internal/clirun and use them from both.",
  "refs": [
    "S16"
  ],
  "history": [
    {
      "date": "2026-10-19",
      "event": "ask"
    },
    {
      "date": "2026-10-19",
      "event": "decide"
    }
  ]
}
---
