---
{
  "id": 100,
  "title": "May codex run write-capable roles in v1, replacing the read-only-only decision (#54)?",
  "status": "decided",
  "key": "vendor.codex-write",
  "asked_by": "claude",
  "decided_by": "jon",
  "created": "2026-10-20",
  "decided_at": "2026-10-20",
  "context": "Owner interview 2026-10-20. #54 limited codex to read-only roles because it has no per-tool allowlist. Hand-driven relays already run Codex implementers with -s workspace-write (agents/setup/lib/workflow-vendor.sh:33-35; S22, S23). ork's engine has only the claude adapter.",
  "options": [
    {
      "label": "workspace-write",
      "summary": "codex implementers run with -s workspace-write and no tool allowlist; the sandbox, checks, review and gate are the limits; bends launch.permissions (#16) for codex"
    },
    {
      "label": "read-only-roles",
      "summary": "keep #54"
    }
  ],
  "recommendation": {
    "label": "workspace-write"
  },
  "decision": "workspace-write",
  "rationale": "Owner 2026-10-20: codex/sol can have write access. Enable it in the engine: a codex adapter with -s workspace-write, a measured sandbox spike (paid call needs the owner's OK), the project check run outside the role, and fallback to the Claude row on rate limit or error.",
  "supersedes": [
    54
  ],
  "refs": [
    "S01",
    "S23"
  ],
  "history": [
    {
      "date": "2026-10-20",
      "event": "ask"
    },
    {
      "date": "2026-10-20",
      "event": "decide"
    },
    {
      "date": "2026-10-20",
      "event": "supersede",
      "note": "supersedes 54"
    }
  ]
}
---
