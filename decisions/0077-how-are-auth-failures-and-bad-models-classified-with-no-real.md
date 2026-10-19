---
{
  "id": 77,
  "title": "How are auth failures and bad models classified with no real sample?",
  "status": "decided",
  "key": "vendor.unsampled-classes",
  "asked_by": "claude",
  "decided_by": "jon",
  "created": "2026-10-19",
  "decided_at": "2026-10-19",
  "context": "No auth-failure sample exists; the bad-model sample (S01 fixture 06) exits 1 with api_error_status 404.",
  "options": [
    {
      "label": "unclassified",
      "summary": "Unclassified until sampled"
    },
    {
      "label": "proposed-401",
      "summary": "A proposed 401 auth rule"
    },
    {
      "label": "both",
      "summary": "Proposed 401 auth and 404 bad-model classes"
    }
  ],
  "decision": "unclassified",
  "rationale": "Owner 2026-10-19: unclassified until a real sample exists, as with rate limits; the owner marks it and the sample becomes a fixture. No synthetic 401 rule.",
  "refs": [
    "S16"
  ],
  "history": [
    {
      "date": "2026-10-19",
      "event": "ask"
    },
    {
      "date": "2026-10-19",
      "event": "decide"
    }
  ]
}
---
