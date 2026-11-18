S343 (adj 307, 308, 315): read-only claude-local roles use a fixed read-only
tool allowlist with permission prompts disabled, avoiding plan mode's
model-backed classifier. Local briefs request targeted reads and brief
reasoning, and local roles get 90 minutes of active runtime. Cloud roles
keep their existing permissions, briefs and timeout.
