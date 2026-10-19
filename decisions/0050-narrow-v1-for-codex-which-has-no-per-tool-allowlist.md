---
{
  "id": 50,
  "title": "Narrow v1 for codex, which has no per-tool allowlist?",
  "status": "open",
  "key": "vendor.codex-allowlist",
  "asked_by": "s01 implementer",
  "created": "2026-10-18",
  "context": "launch.permissions is decided as a per-project, per-role tool allowlist (adj show 16). codex exec has no per-tool allowlist. Its help lists only sandbox modes: read-only, workspace-write, danger-full-access (codex exec --help). The S01 spike ran one codex probe with -s read-only (testdata/vendors/codex/03-readonly-sandbox-write.stdout and .repo-status). That run contains only agent_message items and no command item, so the sandbox was not exercised: the model said the filesystem was read-only and no file was created. The spike did not measure whether codex's sandbox blocks a write. grok's per-tool allowlist is also unmeasured. So codex cannot be shown to meet the allowlist rule for write-capable roles.",
  "options": [
    {
      "label": "read-only-roles",
      "summary": "codex runs only read-only roles (reviewer, gate, architect); implementers run on claude (grok's allowlist is not yet measured)",
      "tradeoffs": "keeps the decided allowlist rule for every write-capable role; codex cannot implement or fall back into an implementer role in v1"
    },
    {
      "label": "workspace-write",
      "summary": "codex implementers run on workspace-write with no tool allowlist; checks and the read-only fingerprint are the only limit",
      "tradeoffs": "codex can implement; the per-role allowlist decision (launch.permissions) must be amended for codex"
    },
    {
      "label": "no-codex-v1",
      "summary": "codex is out of v1 until it can enforce a per-tool allowlist",
      "tradeoffs": "narrows v1 to claude and grok; the three-vendor goal waits"
    }
  ],
  "recommendation": {
    "label": "read-only-roles",
    "reasoning": "It keeps the decided allowlist rule for every write-capable role and uses the one codex control the spike verified (the read-only sandbox)."
  },
  "refs": [
    "S01"
  ],
  "history": [
    {
      "date": "2026-10-18",
      "event": "ask"
    },
    {
      "date": "2026-10-18",
      "event": "amend",
      "note": "context, summary read-only-roles",
      "was": {
        "context": "launch.permissions is decided as a per-project, per-role tool allowlist (adj show 16). codex exec has no per-tool allowlist. Its help lists only sandbox modes: read-only, workspace-write, danger-full-access (codex exec --help). The S01 spike ran codex with -s read-only: a shell write was refused and no file was created (testdata/vendors/codex/03-readonly-sandbox-write.stdout and .repo-status). So codex cannot meet the allowlist rule for write-capable roles.",
        "options": [
          {
            "label": "read-only-roles",
            "summary": "codex runs only read-only roles (reviewer, gate, architect); implementers run on claude or grok"
          }
        ]
      }
    }
  ]
}
---
