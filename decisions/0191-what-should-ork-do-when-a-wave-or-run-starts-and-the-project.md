---
{
  "id": 191,
  "title": "What should ork do when a wave or run starts and the project has no check command?",
  "status": "open",
  "key": "launch.no-check-command",
  "asked_by": "claude",
  "created": "2026-11-07",
  "context": "The 2026-11-04 prism-ui break: two landings each passed with only slicer check, then the base stopped building. The owner chose on 2026-11-07 (S265 interview) to refuse unless an opt-out flag is given.",
  "options": [
    {
      "label": "refuse",
      "summary": "Refuse the start with a message naming the fix, unless --allow-no-check is given"
    },
    {
      "label": "warn",
      "summary": "Start with a warning in the plan and in doctor"
    },
    {
      "label": "brief",
      "summary": "Start, and tell the reviewer and gate that no project check ran"
    }
  ],
  "recommendation": {
    "label": "refuse",
    "reasoning": "The owner's 2026-11-07 answer; a silent pass with no tests broke the base on 2026-11-04."
  },
  "refs": [
    "S265"
  ],
  "history": [
    {
      "date": "2026-11-07",
      "event": "ask"
    }
  ]
}
---
