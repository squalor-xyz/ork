---
{
  "id": 466,
  "title": "Tool feedback (feature, ork): allow removing a file the slice moves out of (rm or git rm) in the implementer's allowed c",
  "status": "decided",
  "key": "tool-feedback.ork-548e367a",
  "decided_by": "Jon",
  "created": "2026-11-18",
  "decided_at": "2026-11-18",
  "context": "Proposal: allow removing a file the slice moves out of (rm or git rm) in the implementer's allowed commands, so a moved file can be deleted instead of left as a comment-only stub\nRole: implementer\nRun: ork-20261118-130342-787e\nProject: ork\nItem: S431\nEvidence: cmd/ork/start_unix.go and start_other.go held only `detach`, which moved to internal/daemon; no allowed form could delete them",
  "options": [
    {
      "label": "file",
      "summary": "ork files it as a slice in ork"
    },
    {
      "label": "drop",
      "summary": "not acted on"
    }
  ],
  "recommendation": {
    "label": "file",
    "reasoning": "A role proposed it; code cannot judge its cost."
  },
  "decision": "file",
  "rationale": "Owner chose 'File it (Recommended)' in an interactive question, relay session 8d711f67, 2026-11-18.",
  "refs": [
    "S431"
  ],
  "history": [
    {
      "date": "2026-11-18",
      "event": "ask"
    },
    {
      "date": "2026-11-18",
      "event": "decide"
    }
  ]
}
---
