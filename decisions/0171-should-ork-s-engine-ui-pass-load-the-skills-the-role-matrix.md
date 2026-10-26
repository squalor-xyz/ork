---
{
  "id": 171,
  "title": "Should ork's engine UI pass load the skills the role matrix names?",
  "status": "open",
  "key": "roles.ui-pass-skills",
  "tags": [
    "roles",
    "ui"
  ],
  "asked_by": "Claude (for jon)",
  "created": "2026-10-26",
  "context": "Owner, 2026-10-26 (session a2bf7e0e): the UI pass should use the tui-design and frontend-design skills, and ork should also support vendor-neutral prompting. Relays now load both skills (rule u10, commit 792e5a2). ork's engine UI prompt is vendor-neutral by test (internal/roles/roles_test.go:496), and Codex cannot load Claude plugin skills. The owner also chose that on a size-S slice the reviewer does the UI pass itself; adj 165 (roles.ui-pass, ui-pass-with-renders) decided a separate engine role. slicer s401 carries the matching contract note.",
  "options": [
    {
      "label": "both",
      "summary": "Claude UI pass roles get a line naming the matrix's skills; every vendor still gets ork's vendor-neutral UI prompt as the base text",
      "tradeoffs": "One prompt plus an optional skills line; the skills line is the only vendor-specific text and comes from project config, not ork's prompt"
    },
    {
      "label": "neutral-only",
      "summary": "Keep the vendor-neutral prompt for every vendor; no skills",
      "tradeoffs": "Simplest; engine UI passes miss the skills' checklists that relays now use"
    },
    {
      "label": "fold-checklist",
      "summary": "Copy the skills' checklists into ork's own neutral prompt (tui-design is MIT; frontend-design's license to check first)",
      "tradeoffs": "Same text for every vendor; ork must track upstream skill changes by hand"
    }
  ],
  "recommendation": {
    "label": "both",
    "reasoning": "It matches the owner's ask for both skills and vendor-neutral prompting, and keeps vendor-specific text in project config. Whether the engine also folds the UI pass into the reviewer on size S follows adj 165 and needs its own answer."
  },
  "refs": [
    "S117",
    "S121"
  ],
  "history": [
    {
      "date": "2026-10-26",
      "event": "ask"
    }
  ]
}
---
