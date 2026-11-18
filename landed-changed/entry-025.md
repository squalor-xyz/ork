S242: add `ork daemon status` for daemon build, pid, stale state, running
roles and behind-main state without starting a daemon. Doctor always prints
a daemon line and shows behind-main while roles run. CLI stale checks ignore
clean revision differences proven to change only non-binary inputs;
embedded prompts still count. Unknown, modified or unverifiable builds keep
the existing warning. The TUI marker keeps its existing rule.
