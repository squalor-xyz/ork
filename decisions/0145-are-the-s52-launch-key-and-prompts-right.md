---
{
  "id": 145,
  "title": "Are the S52 launch key and prompts right?",
  "status": "open",
  "key": "review.s52-launch",
  "asked_by": "claude",
  "created": "2026-10-21",
  "context": "S52 chose key l (Roadmap view only), prompts 'Well specified? [y/n]:' and 'Split? [y/n]:', and the confirm bar 'ork run PROJECT ITEM? [y/N] (Enter is No)', which omits the two answers (run as shown, the CLI would ask them again). Human check: launch one ready slice from the TUI on a test project and confirm the shown command matches ork run.",
  "options": [
    {
      "label": "ok",
      "summary": "Keep l, the prompt wording and the confirm bar",
      "tradeoffs": "Closes the S52 human check."
    },
    {
      "label": "change",
      "summary": "Change the key or wording; file a follow-up",
      "tradeoffs": "Costs a follow-up; l is also drawn as launch wave on the unbuilt Waves screen in dashboard.md."
    }
  ],
  "recommendation": {
    "label": "ok",
    "reasoning": "Gate passed; these were the spec's draft defaults."
  },
  "refs": [
    "S52"
  ],
  "history": [
    {
      "date": "2026-10-21",
      "event": "ask"
    }
  ]
}
---
