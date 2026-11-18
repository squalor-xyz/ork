S370: a permission question keeps a title of at most 120 characters and
carries the whole tool input (the command, else the path) in a new `detail`
field. `ork status --json` has it on the run's question, and `ork show RUN`
prints it in full under `Input:` while the call waits. Docs updated:
`docs/design.md`.
