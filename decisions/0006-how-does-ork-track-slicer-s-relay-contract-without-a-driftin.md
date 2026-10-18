---
{
  "id": 6,
  "title": "How does ork track slicer's relay contract without a drifting copy?",
  "status": "decided",
  "key": "boundary.contract",
  "asked_by": "claude",
  "decided_by": "jon",
  "created": "2026-10-18",
  "decided_at": "2026-10-18",
  "context": "slicer prints Contract: relay v7 (RELAY_CONTRACT in slicer/src/slicer/ai.py). The agents-repo orchestrator role was a full v4 copy until slicer s373.",
  "options": [
    {
      "label": "version-check",
      "summary": "Read the version from slicer ai relay --json and refuse on mismatch; keep no contract text"
    },
    {
      "label": "pinned-copy",
      "summary": "Embed a pinned copy of the contract and update it per release"
    }
  ],
  "recommendation": {
    "label": "version-check",
    "reasoning": "The agents repo doctor already uses the version-marker pattern; a copy drifted before."
  },
  "decision": "version-check",
  "rationale": "Owner interview 2026-10-18: pin to a contract version and refuse on mismatch; also file a slicer item for the machine-readable contract parts in ai relay --json.",
  "history": [
    {
      "date": "2026-10-18",
      "event": "ask"
    },
    {
      "date": "2026-10-18",
      "event": "decide"
    }
  ]
}
---
