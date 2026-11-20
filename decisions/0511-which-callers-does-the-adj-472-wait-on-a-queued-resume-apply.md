---
{
  "id": 511,
  "title": "Which callers does the adj 472 wait on a queued resume apply to?",
  "status": "decided",
  "key": "cli.wait-queued-resume-scope",
  "asked_by": "Jon",
  "decided_by": "Jon",
  "created": "2026-11-19",
  "decided_at": "2026-11-19",
  "context": "The TUI and --wait both send NoWait false (internal/cli/run.go:429, internal/daemon/client.go:265,271).",
  "options": [
    {
      "label": "cli-only",
      "summary": "A wire field that only answer --wait and retry --wait set; the TUI is unchanged"
    },
    {
      "label": "all",
      "summary": "Every caller with NoWait false"
    },
    {
      "label": "tui-nowait",
      "summary": "The TUI sends NoWait true"
    }
  ],
  "recommendation": {
    "label": "cli-only",
    "reasoning": "Recommended in the interactive question."
  },
  "decision": "cli-only",
  "rationale": "Owner chose 'CLI --wait only (Recommended)' in an interactive question, relay session 8d711f67, 2026-11-19.",
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
