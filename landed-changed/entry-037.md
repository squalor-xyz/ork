S356: claude-local uses manual mode and an ork hook for every tool call.
The hook enforces worktree boundaries and a 500-line Read cap, checks the
role allowlist, and blocks uncovered calls on an owner answer without
pausing the run. Hook denials are recorded even when Claude omits them.
Local spec checks ask through their pending Roadmap/Waves sitting. The
path rule permits each role's assigned temp directory and exact report
file while keeping its allowlist and Read cap.
