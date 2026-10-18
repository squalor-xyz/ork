---
{
  "id": 5,
  "title": "Does ork absorb the agents repo relay scripts or call them?",
  "status": "decided",
  "key": "boundary.agents-scripts",
  "asked_by": "claude",
  "decided_by": "jon",
  "created": "2026-10-18",
  "decided_at": "2026-10-18",
  "context": "Owner decided on 2026-10-18 that ork launches and supervises the orchestrator, with the owner attending. Open: how. agents/multi-agent-workflows has relay-call.sh (headless vendor launch, mailbox, read-only fingerprint) and orchestrate.sh (scripted --once run).",
  "options": [
    {
      "label": "call",
      "summary": "ork calls relay-call.sh and leaves vendor launch in the agents repo"
    },
    {
      "label": "absorb",
      "summary": "ork reimplements vendor launch in Go and the scripts are retired"
    },
    {
      "label": "neither",
      "summary": "ork never launches agents; launch stays with the session"
    }
  ],
  "decision": "absorb",
  "rationale": "Owner interview 2026-10-18: ork is N6; it reimplements the vendor layer in Go (fallback, fingerprint, reports); agents repo scripts are retired once ork works.",
  "history": [
    {
      "date": "2026-10-18",
      "event": "ask"
    },
    {
      "date": "2026-10-18",
      "event": "amend",
      "note": "context",
      "was": {
        "context": "agents/multi-agent-workflows has relay-call.sh (headless vendor launch, mailbox, read-only fingerprint) and orchestrate.sh (scripted --once run). Two orchestration paths would coexist otherwise."
      }
    },
    {
      "date": "2026-10-18",
      "event": "decide"
    }
  ]
}
---
