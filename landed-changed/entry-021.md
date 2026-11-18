S369: a cloud Claude UI pass can write its renders. It no longer runs in plan
mode (a probe showed plan mode refuses a write even under an allow rule) or
accept-edits mode (it auto-approves shell file commands in the worktree).
It runs on the reviewer's read-only allowlist plus Skill, and Edit and Write
under its own run temp directory, with `--permission-prompts none`, so every
other call is denied (adj 372, 374). The reviewer, architect, gate and spec check keep
plain plan mode.
