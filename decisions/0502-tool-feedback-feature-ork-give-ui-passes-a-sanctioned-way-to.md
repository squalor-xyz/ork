---
{
  "id": 502,
  "title": "Tool feedback (feature, ork): give UI passes a sanctioned way to render CLI text views, such as a test flag that writes ",
  "status": "decided",
  "key": "tool-feedback.ork-78468b20",
  "decided_by": "Jon",
  "created": "2026-11-19",
  "decided_at": "2026-11-19",
  "context": "Proposal: give UI passes a sanctioned way to render CLI text views, such as a test flag that writes plain output to a given directory\nRole: ui\nRun: ork-20261119-143743-1548\nProject: ork\nItem: S340\nEvidence: CLI status renders had to be copied from a test's want string because internal/cli cannot be imported from outside the module and the tree is read-only",
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
  "rationale": "Owner chose 'Reconsider: file a render flag slice' in an interactive question, relay session 8d711f67, 2026-11-19.",
  "refs": [
    "S340"
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
