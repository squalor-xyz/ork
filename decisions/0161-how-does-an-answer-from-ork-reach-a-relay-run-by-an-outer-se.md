---
{
  "id": 161,
  "title": "How does an answer from ork reach a relay run by an outer session?",
  "status": "open",
  "key": "relay.answer-channel",
  "created": "2026-10-25",
  "context": "Owner 2026-10-25: a question raised by a subagent or sub-orchestrator should bubble up to ork (on Overview, the project page and the Running summary), be answered there with the provided recommended options, and be sent back down so the run continues. ork's own engine runs already pause and take ork answer. A relay run by an outer Claude session (a 'started elsewhere' ledger run) has no channel from ork back to that session. ork may use slicer only through its CLI and JSON (goal C03) and cannot write ../slicer, so any channel must be a slicer feature. The agents repo's relay-call.sh mailbox (.relay/SLICE/ in the worktree) carries role briefs and replies between an orchestrator and its roles, not owner answers.",
  "options": [
    {
      "label": "ledger-ask",
      "summary": "slicer adds relay-run ask and relay-run answer: a step records a question with options and a recommendation in the ledger; ork reads it from relay-run list --json and writes the answer with relay-run answer RUN STEP TEXT; the orchestrator polls relay-run answer RUN and continues",
      "tradeoffs": "Fits goal C03 and the shared ledger every tool already reads; needs a slicer feature, a relay contract bump and orchestrators that poll; the contract is not ork's to change."
    },
    {
      "label": "file-mailbox",
      "summary": "ork writes an answer file under the worktree .relay directory and the orchestrator polls it",
      "tradeoffs": "No slicer change, but ork writes into worktrees it does not own and the path convention lives in the agents repo."
    },
    {
      "label": "engine-only",
      "summary": "Only ork's own engine runs can be answered; external relays stay read-only",
      "tradeoffs": "No cross-repo work; external relays keep asking the outer session."
    }
  ],
  "recommendation": {
    "label": "ledger-ask",
    "reasoning": "Keeps ork on slicer's CLI (C03) and uses the one ledger all tools share; the owner asked for a mailbox in principle on 2026-10-25, and this is the mechanism that fits ork's boundaries."
  },
  "refs": [
    "S28"
  ],
  "history": [
    {
      "date": "2026-10-25",
      "event": "ask"
    }
  ]
}
---
