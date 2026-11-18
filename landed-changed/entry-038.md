*Proposed (S358):* claude-local roles start a fresh session when current
input context exceeds 70,000 tokens, including cached input. Restarts keep
the original brief, a role-written scratch progress note and the worktree
diff stat. One role row and the 90-minute runtime limit span all sessions;
the run records each restart's time and token count. Read-only local roles
get an exact-file write exception for their progress note. Cloud roles do
not change. Docs updated: `docs/design.md` and `docs/state.md`.
