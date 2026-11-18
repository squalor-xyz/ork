S179 (*proposed*): Spec drafting. `D` in the Roadmap item pane and `ork spec
draft PROJECT ID` ask a read-only role, the new `spec-draft` role on the
spec-check row, to propose new text for a slice's Why, Files, Failing tests,
Implement and Check sections; it may draft every section from the title alone.
Both show each section's current text and its proposal. Nothing is written
until the owner approves a section (`y` in the TUI, `y` at the CLI prompt,
which needs a terminal); ork then runs `slicer edit ID --section NAME --text
... --render` for that section, after it checks that the spec did not change
since the draft. A Files path that is not in the checkout, and not marked
`(new)`, is a problem and blocks that section. The daemon methods are
`spec.draft` and `spec.draft-apply`; `ork roles print spec-draft` prints the
prompt. Owner decisions 2026-11-16: `scope.spec-drafting` (lifts the wait of
`scope.v1` for spec text), `roles.spec-draft-row`, `roles.spec-draft-empty`,
`ui.spec-draft-key`, `cli.spec-draft`.
