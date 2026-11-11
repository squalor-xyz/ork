---
{
  "id": 322,
  "title": "Which command sets the owner, given `ork config` does not exist?",
  "status": "decided",
  "key": "owner.setting-command",
  "asked_by": "Jon",
  "decided_by": "Jon",
  "created": "2026-11-11",
  "decided_at": "2026-11-11",
  "context": "Question: Which command sets the owner, given `ork config` does not exist?\nOptions:\n1. settings: extend the existing `ork settings set` with `--owner NAME` and `--file-answers on|off`, and `ork settings show` prints both. The notice and tests then say `ork settings set --owner NAME`.\n2. config: add a new top-level `ork config set KEY VALUE` and `ork config show`, so `ork settings` and `ork config` both write file-level settings.\n3. config over settings: add `ork config set owner NAME` as a thin front for the same stored settings, and keep `ork settings` for the caps.\nRecommend: 1 - `ork settings` already writes the registry's file-level settings (`internal/cli/settings.go`, `internal/cli/cli.go:385`), and adj 228 settles where the name comes from (a global ork setting) but not the command word.\nDecision key: owner.setting-command",
  "decision": "settings: extend the existing `ork settings set` with `--owner NAME` and `--file-answers on|off`, and `ork settings show` prints both. The notice and tests then say `ork settings set --owner NAME`.",
  "rationale": "Answered in ork on 2026-11-11: Which command sets the owner, given `ork config` does not exist?",
  "refs": [
    "S314"
  ],
  "history": [
    {
      "date": "2026-11-11",
      "event": "ask"
    },
    {
      "date": "2026-11-11",
      "event": "decide"
    }
  ]
}
---
