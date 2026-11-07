---
{
  "id": 190,
  "title": "Should ork projects set --allow add to a role's allowlist instead of replacing it?",
  "status": "decided",
  "key": "cli.allow-adds",
  "asked_by": "claude",
  "decided_by": "Jon",
  "via": "interactive question, session 8d711f67",
  "created": "2026-11-07",
  "decided_at": "2026-11-07",
  "context": "Today --allow replaces the role's list (S174 decision of 2026-10-29), so a second call drops the first pattern and a list can lose Read or Edit. The owner chose 'add' on 2026-11-07 (S265 interview). Choosing it reverses the S174 decision for --allow only.",
  "options": [
    {
      "label": "add",
      "summary": "--allow adds to the effective list and skips duplicates; --replace-allow replaces it"
    },
    {
      "label": "add-flag",
      "summary": "Keep replace; add --add-allow ROLE=PATTERN"
    },
    {
      "label": "warn",
      "summary": "Keep replace; warn when the result lacks Read, Edit or Write"
    }
  ],
  "recommendation": {
    "label": "add",
    "reasoning": "Matches the friction found on 2026-11-03 and the owner's 2026-11-07 answer."
  },
  "decision": "add",
  "rationale": "Owner picked 'add' in the interactive question on 2026-11-07 (relay session).",
  "refs": [
    "S265",
    "S174"
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
