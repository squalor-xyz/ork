---
{
  "id": 106,
  "title": "Should run.json be written 0600 now that it can hold a denied command?",
  "status": "decided",
  "key": "state.run-json-mode",
  "asked_by": "claude",
  "decided_by": "jon",
  "created": "2026-10-20",
  "decided_at": "2026-10-20",
  "context": "S45 review: internal/runrec/store.go:281 writes run.json at 0644. A recorded denied command (capped at 200 characters, not scrubbed) may hold a secret. The raw stdout of a denied round is already saved at 0600 (docs/design.md:372). Also open: ESC and other control characters in a recorded command reach ork status and the TUI; #105 replaces only CR and LF.",
  "options": [
    {
      "label": "0600",
      "summary": "write run.json (and the run directory files) at 0600",
      "tradeoffs": "matches the stdout files; one-line change plus a test"
    },
    {
      "label": "keep",
      "summary": "keep 0644; the file is in the owner's own state directory",
      "tradeoffs": "no change; other local users can read commands"
    },
    {
      "label": "0600-strip",
      "summary": "0600 and strip all control characters from a recorded command",
      "tradeoffs": "also stops terminal escape sequences in status and the TUI; a little more code"
    }
  ],
  "recommendation": {
    "label": "0600-strip",
    "reasoning": "A command can hold a secret and can hold escape sequences; both fixes are small."
  },
  "decision": "0600-strip",
  "rationale": "Owner 2026-10-20: make it so. Write run.json and the run directory files at 0600, and strip all control characters from a recorded command.",
  "refs": [
    "S45",
    "105"
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
