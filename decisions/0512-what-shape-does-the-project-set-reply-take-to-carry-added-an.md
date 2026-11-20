---
{
  "id": 512,
  "title": "What shape does the project.set reply take to carry added and already-listed pattern counts?",
  "status": "decided",
  "key": "cli.project-set-reply-shape",
  "asked_by": "Jon",
  "decided_by": "Jon",
  "created": "2026-11-19",
  "decided_at": "2026-11-19",
  "context": "setProject and Client.SetProject return registry.Project today (internal/daemon/handlers.go:666, internal/daemon/client.go:71).",
  "options": [
    {
      "label": "wrapper",
      "summary": "A new result struct with the project plus per-role added and already_listed counts; C2's upgrade note covers older CLIs"
    },
    {
      "label": "fields",
      "summary": "omitempty count fields beside the project fields"
    },
    {
      "label": "summary",
      "summary": "The CLI computes counts from a pre-read"
    }
  ],
  "recommendation": {
    "label": "wrapper",
    "reasoning": "Recommended in the interactive question."
  },
  "decision": "wrapper",
  "rationale": "Owner chose 'New result struct (Spec check's pick)' in an interactive question, relay session 8d711f67, 2026-11-19.",
  "refs": [
    "S307"
  ],
  "history": [
    {
      "date": "2026-11-19",
      "event": "ask"
    },
    {
      "date": "2026-11-19",
      "event": "decide"
    }
  ]
}
---
