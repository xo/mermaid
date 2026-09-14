# Documentation example corpus

One diagram per type, copied verbatim from the headline example on the
corresponding [Mermaid documentation](https://mermaid.js.org) page, so that a
renderer upgrade can be judged against what Mermaid itself advertises rather
than against fragments invented here.

The numbering follows the order the documentation lists the diagram types in.
`32-other-*.mmd` come from the "Other Examples" page, which is a gallery rather
than a diagram type.

Sources are `packages/mermaid/src/docs/syntax/*.md` in
[mermaid-js/mermaid](https://github.com/mermaid-js/mermaid), taken from commit
`3f5f7a6781cc8c788b8b5fa3d4e62edce9288f60` (2026-09-12, mermaid 12.0.0).
`scripts/fetch-examples.sh` refreshes them.

`examples_test.go` holds the manifest: which documentation page each file came
from, and whether the pinned renderer actually understands it. Six of these
diagram types are not implemented by mermaid-rs-renderer at all and are
silently parsed as flowcharts; one is rejected outright. See the "Diagram
types" section of the top-level README.
