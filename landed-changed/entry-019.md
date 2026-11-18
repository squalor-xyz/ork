S373: the run pane of a permission question shows the call's whole input
(the question's `detail`) under the title, wrapped at the pane width with a
2-space hanging indent on a wrapped line, so the owner can read every token
of a command before an allow (adj 369, 378). On a short terminal a detail
that does not fit drops whole and a line says `+N detail lines hidden; ork
show RUN`. A question with no detail looks as before. Docs updated:
`docs/dashboard.md`.
