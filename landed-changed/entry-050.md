Polling (S336, adj 301): with no TUI attached the daemon reads nothing on a
timer, and a TUI that attaches reads the parts of the projects it shows that are due at once. Merge
detection rides on the git HEAD read, so with no TUI attached it waits until
one attaches (owner decision 2026-11-11). With a TUI attached, a live run is
read every 2 s instead of every 1 s.
