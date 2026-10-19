---
{
  "id": 53,
  "title": "Before the daemon (S03), who runs the slicer, adj and git calls?",
  "status": "decided",
  "key": "dash.pre-daemon",
  "asked_by": "claude",
  "decided_by": "jon",
  "created": "2026-10-18",
  "decided_at": "2026-10-18",
  "context": "S02 builds the dashboard; S03 adds the daemon and socket.",
  "options": [
    {
      "label": "direct",
      "summary": "TUI calls adapters behind one interface"
    },
    {
      "label": "daemon-first",
      "summary": "build S03 before the dashboard"
    }
  ],
  "decision": "direct",
  "rationale": "Owner 2026-10-19: The TUI calls the adapters directly behind one Go interface, which S03 moves behind the socket unchanged.",
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
