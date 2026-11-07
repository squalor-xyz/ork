---
{
  "id": 189,
  "title": "Should launch stop asking well specified and split, and default them to yes and no?",
  "status": "decided",
  "key": "launch.well-specified-default",
  "asked_by": "claude",
  "decided_by": "Jon",
  "via": "interactive question, session 8d711f67",
  "created": "2026-11-07",
  "decided_at": "2026-11-07",
  "context": "Adj 86 (run.lineup-inputs), 87 (run.prompt-answers) and 135 (ui.wave-composer) make ork ask both inputs for every item with no default. The owner answered on 2026-11-07 (S260 interview) that the prompts are friction. The spec check already decides what well specified means (rule u4) and holds items that are not Ready. Answering 'replace' means the owner supersedes 86, 87 and 135; ork never runs supersede.",
  "options": [
    {
      "label": "replace",
      "summary": "Plan every item as well specified = yes and split = no; --well-specified and --split stay as overrides; --yes needs no flags; the TUI asks nothing on tick"
    },
    {
      "label": "defaults",
      "summary": "Keep the prompts, but Enter accepts yes and no"
    },
    {
      "label": "keep",
      "summary": "Keep adj 86, 87 and 135 as decided; retire S260"
    }
  ],
  "recommendation": {
    "label": "replace",
    "reasoning": "The spec check already holds items that are not Ready, so the answer would go unused for items that start. The owner chose replace in the 2026-11-07 interview."
  },
  "decision": "replace",
  "rationale": "Owner picked 'replace' in the interactive question on 2026-11-07 (relay session).",
  "refs": [
    "S260",
    "86",
    "87",
    "135"
  ],
  "history": [
    {
      "date": "2026-11-07",
      "event": "ask"
    },
    {
      "date": "2026-11-07",
      "event": "decide"
    }
  ]
}
---
