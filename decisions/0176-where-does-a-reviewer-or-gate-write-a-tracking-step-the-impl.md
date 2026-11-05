---
{
  "id": 176,
  "title": "Where does a reviewer or gate write a tracking step the implementer was denied?",
  "status": "decided",
  "key": "roles.denied-tracking-step",
  "asked_by": "Claude",
  "decided_by": "Jon",
  "created": "2026-11-05",
  "decided_at": "2026-11-05",
  "context": "S277 spec check held the slice on this question; the denied step (for example slicer note in the finish step) is the orchestrator's to run.",
  "options": [
    {
      "label": "1",
      "summary": "Commentary line: one untagged line under section (C) for the reviewer; the gate gets one 'Deviation for the orchestrator' evidence line beside its criterion lines. Neither is a finding, so neither starts a fix round."
    },
    {
      "label": "2",
      "summary": "Tagged [later]: a later finding offered to the owner as a follow-up slice."
    },
    {
      "label": "3",
      "summary": "Tagged [owner]: an owner finding with options."
    }
  ],
  "recommendation": {
    "label": "1",
    "reasoning": "The reviewer prompt reads untagged text in section (C) as commentary (Triage), and the gate prompt has no fix-free slot, so no parser or shape change is needed."
  },
  "decision": "1",
  "rationale": "Owner picked option 1 in the ork session on 2026-11-05 ('1, go ahead') after the S277 spec check held the slice on this question.",
  "refs": [
    "S277"
  ],
  "history": [
    {
      "date": "2026-11-05",
      "event": "ask"
    },
    {
      "date": "2026-11-05",
      "event": "decide"
    }
  ]
}
---
