*Proposed (S360):* a claude-local role starts
with less fixed context: ork passes `--disallowedTools` (WebFetch,
WebSearch, Task, Agent, NotebookEdit, and Skill except for a UI pass) and
`--disable-slash-commands` (a UI pass omits this flag too, so it keeps its
skills), and sets `BASH_MAX_OUTPUT_LENGTH` in its
environment to cap one Bash result. The cap is 10000 characters (owner
decision 2026-11-12); output over the cap is saved to a file and the role
sees a short preview. Cloud roles do not change.
