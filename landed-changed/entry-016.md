S247 (*proposed*): `ork projects show NAME [--json]` and the daemon method
`projects.show` print every setting of one project in one read: the registry
entry, ork's global settings, slicer's config and role matrix (read through
`slicer config --json` and `slicer relay-roles show --json`) and adj's state
(the registry flag, and whether `.adjudicate.json` is a regular file). A part
slicer cannot read is null with slicer's code and message, and the call still
succeeds. Text has one block per owner and leaves out `vendor_env` values;
`--json` prints them as `projects list --json` does (S192 decides masking
later). It writes nothing. Docs updated: `docs/design.md`, `docs/guide.md`.
