---
{
  "id": 131,
  "title": "How is the ork logo shown in the terminal?",
  "status": "decided",
  "key": "ui.logo-text-mark",
  "asked_by": "claude",
  "decided_by": "jon",
  "created": "2026-10-20",
  "decided_at": "2026-10-20",
  "context": "Owner asked what it takes to get the logo in the terminal (WezTerm: kitty, iTerm2 and sixel image protocols exist). Supersedes the one-line doctor mark in record 130; the version and stale-daemon parts of 130 stand.",
  "options": [
    {
      "label": "text-mark",
      "summary": "A six-row half-block text mark (ring, lanes, three coloured slices) with a no-colour and an ASCII fallback, shown in ork --version, ork doctor and the top of the ? help screen; no launch splash; no real-image protocol in this slice"
    },
    {
      "label": "text-mark-splash",
      "summary": "Same mark plus a launch splash"
    },
    {
      "label": "text-then-image",
      "summary": "Text mark now, a WezTerm image later in version and doctor only"
    }
  ],
  "recommendation": {
    "label": "text-mark",
    "reasoning": "Works in every terminal and adds no wait at start."
  },
  "decision": "text-mark",
  "rationale": "Owner 2026-10-20 chose the text mark in version, doctor and help; no splash.",
  "refs": [
    "S54"
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
