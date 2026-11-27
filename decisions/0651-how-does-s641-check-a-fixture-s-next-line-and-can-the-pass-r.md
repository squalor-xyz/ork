---
{
  "id": 651,
  "title": "How does S641 check a fixture's next: line, and can the pass render a preset height?",
  "status": "decided",
  "key": "roles.render-fixture-next-check",
  "asked_by": "Jon",
  "decided_by": "Jon",
  "created": "2026-11-27",
  "decided_at": "2026-11-27",
  "context": "S538 spec draft.",
  "options": [
    {
      "label": "pausednext-render-size",
      "summary": "fixtures derive next: from an exported daemon.PausedNext; add -render-size WxH[,WxH...] writing NAME-WxH.txt"
    },
    {
      "label": "no-size",
      "summary": "no size flag"
    },
    {
      "label": "daemon-table",
      "summary": "daemon-side table"
    }
  ],
  "recommendation": {
    "label": "pausednext-render-size",
    "reasoning": "Recommended in the interactive question."
  },
  "decision": "pausednext-render-size",
  "rationale": "Owner chose 'Export PausedNext + -render-size' in an interactive question, relay session 8d711f67, 2026-11-27.",
  "refs": [
    "S538"
  ],
  "history": [
    {
      "date": "2026-11-27",
      "event": "ask"
    },
    {
      "date": "2026-11-27",
      "event": "decide"
    }
  ]
}
---
