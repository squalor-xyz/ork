---
{
  "id": 490,
  "title": "When the tree is unchanged since a role's last report, does the resumed brief still say to re-run git status and git diff?",
  "status": "decided",
  "key": "brief.tree-unchanged",
  "asked_by": "Jon",
  "decided_by": "Jon",
  "created": "2026-11-18",
  "decided_at": "2026-11-18",
  "context": "internal/engine/round.go:150-160, internal/engine/brief.go:165-172; run ork-20261118-035228-7421.",
  "options": [
    {
      "label": "state",
      "summary": "State the fact and drop the git re-run; keep re-read every file you cite"
    },
    {
      "label": "keep",
      "summary": "State it and keep every instruction"
    },
    {
      "label": "record",
      "summary": "Record it in the run record only"
    }
  ],
  "recommendation": {
    "label": "state",
    "reasoning": "Recommended in the interactive question."
  },
  "decision": "state",
  "rationale": "Owner chose 'State it, drop git re-run (Recommended)' in an interactive question, relay session 8d711f67, 2026-11-18.",
  "refs": [
    "S424"
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
