S335 (*proposed*): Batched snapshot polling. The daemon reads the items,
goals and runs parts of every project that is due with one `slicer snapshot`
process (slicer 1.22.1, snapshot schema 2) instead of one process per part per
project, and a part whose revision did not change is not decoded or announced.
A failed batched call falls back to the per-part calls for that tick, and a
project with an `error` in the reply fails only its own parts. Deps, adj
records and git HEAD stay one call per project.
