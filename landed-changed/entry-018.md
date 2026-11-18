S375: the spec-check screen shows a pending permission question's whole
command (the question's `detail`) under the title, wrapped the same way the
run pane wraps it, with a blank line before the options. A command that
does not leave room for the options, the Why line and the footer is omitted,
and one dim line says `+N detail lines hidden`, so the owner can still allow
or deny (adj 377). Docs updated: `docs/dashboard.md`.
