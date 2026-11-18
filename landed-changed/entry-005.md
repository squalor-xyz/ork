S407 (*proposed*): Slice checks skip the Acceptance criteria list. In a
slice's Check section, entries after a line `Acceptance criteria:` and up to
a line `Commands:` are prose: ork no longer runs a backticked word in them
before the gate, and no longer lists them as Check commands in a brief. Docs
updated: `docs/design.md`.
