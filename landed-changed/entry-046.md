*Proposed (S312):* `ork run` and `ork wave start` print every open spec
question with `--questions`, without planning, recording answers or
starting runs (exit 3 when questions remain, 0 otherwise). Repeat
`--answer KEY=N` or `KEY=text` to answer a whole batch in any order;
waves require `ITEM:KEY=value`, and keyless questions use `#N`.
Invalid or incomplete batches record nothing and start nothing.
JSON question entries include `item`, `address`, `ask`, optional `key`
and `text`; `answered` entries are `{item,key,answer}`. Text listings show
wave answer prefixes, keyless addresses and an explicit empty result.
Existing interactive and piped-input prompts are unchanged.
