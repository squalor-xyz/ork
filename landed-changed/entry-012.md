S378 (*proposed*): a grok round that stops on the free usage limit ("You've
reached your free Grok Build usage limit") is classed out, like out of
credits, so the role falls back to its next row instead of stopping the run
as unclassified. The text arrives in the `errors` of an
`error_during_execution` result, which ork now reads. Fixture:
`testdata/vendors/grok/08-usage-limit`. Docs updated: `docs/design.md`.
