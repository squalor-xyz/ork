S359 (*proposed*): a local implementer takes a slice above size S as
slivers. The spec check that runs before the start has the spec check row
carve the slice once it is Ready (read-only, with its own carve prompt) and
files each sliver with `slicer add --parent PARENT --size S --effort 1` with
no owner confirmation (adj 337, 338). `ork run` and `ork wave start` then plan
again, show the filed slivers as the chain, and the owner approves that chain
once (adj 388, 396), in the TUI too (adj 401); it runs in the parent's worktree (S62). A start never
carves. Slivers that already exist are run, not carved again, except that a
filing that failed part way is finished from the saved carve report by the
next spec check. An effort 3 slice is refused on a local implementer (rule
u6) and a cloud implementer is not slivered. Before the check the plan shows
`carve`. `--questions` starts nothing but still carves, and says which
slivers it filed. Docs updated: `docs/design.md`, `docs/dashboard.md`.
