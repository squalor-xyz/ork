---
{
  "id": 344,
  "title": "How does the blocked `ork hook pretool` process reach the owner and get the answer back (Q2 = block, adj 330)?",
  "status": "decided",
  "key": "roles.local-approval-ask-channel",
  "asked_by": "Jon",
  "decided_by": "Jon",
  "created": "2026-11-13",
  "decided_at": "2026-11-13",
  "context": "Question: How does the blocked `ork hook pretool` process reach the owner and get the answer back (Q2 = block, adj 330)?\nOptions:\n1. daemon socket: the hook process calls a new daemon request carrying the run id (passed in its environment or in the `--settings` command line). The daemon shows a permission question on the run (`ork answer RUN TEXT`, the run pane) and the hook waits for the reply, then prints allow or deny. A timeout set in the hook settings denies. The run keeps running while the question is open. It does not use the `PendingQuestion` pause and resume path.\n2. pause the run: the hook denies and exits at once. The daemon records the call as a `PendingQuestion`, so the run pauses and `ork answer` resumes it with that call allowed. This stays closest to the existing pause and resume path (`internal/daemon/runs.go:1539-1754`), but it is no longer \"the hook blocks\".\n3. file handshake: the hook writes a request file in the run's state directory and polls for an answer file that the daemon writes. It needs no new socket request, but it adds a polling protocol and a new file to `docs/state.md`.\nRecommend: 1 - it matches the decided \"block\", and the owner answers while claude waits. The hook-to-daemon request is a new interface, so the owner picks it before the implementer builds it.\nDecision key: roles.local-approval-ask-channel",
  "decision": "daemon socket: the hook process calls a new daemon request carrying the run id (passed in its environment or in the `--settings` command line). The daemon shows a permission question on the run (`ork answer RUN TEXT`, the run pane) and the hook waits for the reply, then prints allow or deny. A timeout set in the hook settings denies. The run keeps running while the question is open. It does not use the `PendingQuestion` pause and resume path.",
  "rationale": "Answered in ork on 2026-11-13: How does the blocked `ork hook pretool` process reach the owner and get the answer back (Q2 = block, adj 330)? (S356)",
  "refs": [
    "S356"
  ],
  "history": [
    {
      "date": "2026-11-13",
      "event": "ask"
    },
    {
      "date": "2026-11-13",
      "event": "decide"
    }
  ]
}
---
