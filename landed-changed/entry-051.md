*Proposed (S248):* `ork projects set NAME ... --dry-run` runs every check
the real call runs, including the busy check, and prints the project as it
would be saved without saving it. JSON adds `dry_run: true`; the text verb
is `would update`. A call without `--dry-run` is unchanged.
