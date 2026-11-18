*Proposed (S364; adj 351, 352):* a claude-local role gets ork's own short
system prompt (`--system-prompt-file`) in place of Claude Code's default and
only six built-in tools (`--tools Read,Grep,Glob,Edit,Write,Bash`; a
read-only role drops Edit and Write unless it holds its progress-note
grant; a UI pass adds Skill), replacing S360's `--disallowedTools`. The
daemon's `ORK_LOCAL_FRESH_TOKENS` overrides the 70,000-token fresh-session
threshold for a measurement. Cloud roles do not change.
