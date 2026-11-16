---
{
  "id": 386,
  "title": "Which vendor_env form does ork projects show --json print while S192's masking question is open?",
  "status": "decided",
  "key": "projects.show-vendor-env-form",
  "asked_by": "Jon",
  "decided_by": "Jon",
  "created": "2026-11-16",
  "decided_at": "2026-11-16",
  "context": "S247 spec check question. The masking question itself stays open in S192.",
  "options": [
    {
      "label": "unmasked-now",
      "summary": "print vendor_env as projects list --json does today; S192 later masks list, set and show together"
    },
    {
      "label": "mask-now",
      "summary": "decide masking here; projects show --json prints the masked form"
    },
    {
      "label": "wait-s192",
      "summary": "hold S247 until S192 is answered"
    }
  ],
  "recommendation": {
    "label": "unmasked-now",
    "reasoning": "the code does not depend on the form; docs/state.md:306-309 says vendor_env is not a place for secrets; the text output prints no values"
  },
  "decision": "unmasked-now",
  "rationale": "Owner chose 'Unmasked now (Recommended)' in an interactive question, relay session 8d711f67, 2026-11-16.",
  "refs": [
    "S247",
    "S192"
  ],
  "history": [
    {
      "date": "2026-11-16",
      "event": "ask"
    },
    {
      "date": "2026-11-16",
      "event": "decide"
    }
  ]
}
---
