---
{
  "id": 86,
  "title": "Where do well-specified and split come from for ork run?",
  "status": "decided",
  "key": "run.lineup-inputs",
  "asked_by": "claude",
  "decided_by": "jon",
  "created": "2026-10-19",
  "decided_at": "2026-10-19",
  "context": "slicer relay-roles recommend requires --well-specified yes|no and --split yes|no (internal/slicercli/write.go:94); S17 does not say where ork gets them.",
  "options": [
    {
      "label": "prompt",
      "summary": "ask at the confirmation step; require explicit --well-specified and --split flags when --yes skips the prompt"
    },
    {
      "label": "flags",
      "summary": "optional flags with defaults"
    },
    {
      "label": "slice",
      "summary": "derive from the slice data"
    }
  ],
  "decision": "prompt",
  "rationale": "Owner 2026-10-19: ork run prompts for well-specified and split at the confirmation step; with --yes, explicit --well-specified and --split flags are required.",
  "refs": [
    "S17"
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
