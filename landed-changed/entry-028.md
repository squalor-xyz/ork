S313 (*proposed*): reuse fitting decided adj options by exact decision key
during spec checks, record `[KEY, adj N]` in the slice and skip the prompt.
Open records hold daemon, CLI and TUI launches with `waits on adj N`;
stale options show their old answer and date before asking again. Lookup
failures keep the question with a notice. The spec-check prompt searches
decided records before inventing a key.
