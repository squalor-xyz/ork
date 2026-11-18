S396 (*proposed*): `item.park` checks again for an unfinished run after
`slicer park` and undoes the park with `slicer unpark` when a run started in
between. The call stays `refused` and names that run; a failed unpark is
`internal` and says the item is parked although the run is unfinished.
`Note`, `Park`, `Unpark`, `Move`, `Show` and `ShowRaw` refuse an id that
starts with `-`, and the `item.*` handlers refuse it as `bad_params`. Docs
updated: `docs/design.md`.
