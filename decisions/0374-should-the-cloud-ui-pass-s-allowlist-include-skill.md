---
{
  "id": 374,
  "title": "Should the cloud UI pass's allowlist include Skill?",
  "status": "decided",
  "key": "roles.ui-pass-skill-tool",
  "asked_by": "Jon",
  "decided_by": "Jon",
  "created": "2026-11-16",
  "decided_at": "2026-11-16",
  "context": "S369 implementer question under adj 372.",
  "options": [
    {
      "label": "add-skill",
      "summary": "the UI pass allowlist is the read-only list plus Skill plus Edit(TEMPDIR/**)"
    },
    {
      "label": "leave-out",
      "summary": "skills are denied for a cloud UI pass"
    }
  ],
  "recommendation": {
    "label": "add-skill",
    "reasoning": "the UI pass loads the skills enabled as plugins (AGENTS.md, rule u10); Skill changes no file"
  },
  "decision": "add-skill",
  "rationale": "Owner chose 'Add Skill (Recommended)' in an interactive question, relay session 8d711f67, 2026-11-16.",
  "refs": [
    "S369"
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
