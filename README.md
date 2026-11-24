<hr/>

## DEPRECATED

NOTE: This repo has been deprecated. A Pure Go Mermaid parser/renderer is now
available in the [`zuzu`](https://github.com/xo/zuzu) package.

<hr/>

# mermaid

Render [Mermaid](https://mermaid.js.org) diagrams to SVG from Go. Pure Go, no
cgo, no browser, no Node.

Mermaid itself is a JavaScript library that needs a DOM to lay text out, so
rendering it normally means driving a headless browser. This package instead
runs [mermaid-rs-renderer](https://github.com/1jehuang/mermaid-rs-renderer) — a
parser, layout engine and SVG writer written in Rust and published as the
WebAssembly package
[`mermaid-wasm-renderer`](https://www.npmjs.com/package/mermaid-wasm-renderer) —
on [wazero](https://wazero.io). Diagram source goes into WebAssembly linear
memory, an SVG document comes back out.

```go
ctx := context.Background()

svg, err := mermaid.Render(ctx, "flowchart TD\n  A[Start] --> B[Done]")
if err != nil {
    log.Fatal(err)
}

os.WriteFile("diagram.svg", []byte(svg), 0o644)
```

Reuse a `Renderer` when rendering more than one diagram — the WebAssembly
compile dominates the cost of a single render:

```go
r, err := mermaid.New(ctx)
if err != nil {
    log.Fatal(err)
}
defer r.Close(ctx)

for _, src := range sources {
    svg, err := r.Render(ctx, src)
    // ...
}
```

## API

|                                                             |                                                |
| ----------------------------------------------------------- | ---------------------------------------------- |
| `New(ctx, opts...) (*Renderer, error)`                      | Compiles the module. Reuse it.                 |
| `(*Renderer).Render(ctx, src) (string, error)`              | Diagram source to an SVG document.             |
| `(*Renderer).RenderWith(ctx, src, Options) (string, error)` | The same, with a theme and configuration.      |
| `(*Renderer).RegisterFont(ctx, ttf) error`                  | Adds a font for text measurement.              |
| `(*Renderer).Version() string`                              | The bundled renderer release.                  |
| `Render` / `RenderWith`                                     | One-shot helpers that own the runtime for you. |

`Options.Theme` picks one of `default`, `dark`, `forest`, `neutral` or
`modern`. `Options.Config` takes a Mermaid configuration document as JSON — the
same schema the `mmdr` CLI reads from `--config`, with keys such as
`themeVariables`, `flowchart` and `sequence`. `Theme` wins over a `theme` key
inside `Config`.

A syntax error comes back as a Go error carrying the renderer's own diagnostic,
which names the offending token and its position:

```
mermaid: unexpected token 'this is not a diagram' at 1:1; expected unknown or missing Mermaid diagram header
```

## Diagram types

`testdata/examples/` holds the headline example from every Mermaid
documentation page, copied verbatim, so that what this renderer does can be
judged against what Mermaid advertises. `scripts/fetch-examples.sh` refreshes
them; the table in `examples_test.go` records which ones the pinned renderer
actually understands.

Twenty-three of the thirty-one documented diagram types render:

`flowchart` / `graph`, `sequenceDiagram`, `classDiagram`, `stateDiagram-v2`,
`erDiagram`, `journey`, `gantt`, `pie`, `quadrantChart`, `requirementDiagram`,
`gitGraph`, `C4Context`, `mindmap`, `timeline`, `zenuml`, `sankey`, `xychart`,
`block`, `packet`, `kanban`, `architecture`, `radar`, `treemap` — plus the
`-beta` spellings of the beta types.

Eight do not. **Seven of them fail silently**, which is the trap to know about:
the renderer's parser does not recognise the header, falls through to flowchart
syntax, and returns a perfectly well-formed SVG of boxes labelled with
fragments of your source. A `venn-beta` diagram comes back as boxes reading
"venn", "beta" and "set".

| Type              | Header          | What happens           |
| ----------------- | --------------- | ---------------------- |
| Swimlanes Diagram | `swimlane-beta` | parsed as a flowchart  |
| Use Case Diagram  | `usecase-beta`  | parsed as a flowchart  |
| Venn              | `venn-beta`     | parsed as a flowchart  |
| Ishikawa          | `ishikawa-beta` | parsed as a flowchart  |
| Wardley           | `wardley-beta`  | parsed as a flowchart  |
| Cynefin           | `cynefin-beta`  | parsed as a flowchart  |
| TreeView          | `treeView-beta` | parsed as a flowchart  |
| Event Modeling    | `eventmodeling` | rejected with an error |

Mermaid 12 also documents `agentflow-beta` and `railroad-ebnf-beta`, which the
renderer does not implement either; they are not in the corpus.

Nothing in the API distinguishes a fallback from a real render, so if you accept
diagram source from users, reject these headers yourself before calling
`Render`.

## Fidelity

It is a reimplementation rather than a port, so output is _Mermaid-like_ rather
than byte-identical to upstream `mermaid.js`: layout is close, and some details
differ. Entity-relationship cardinality markers and pie-slice labelling are the
most visible divergences in the current release.

**Sankey is broken.** Within a column the renderer only assigns a vertical
position to nodes that have an incoming link, so every source node sits at y=0
on top of the others. On the diagram from the Mermaid Sankey documentation
page, twenty of the forty-eight nodes land on a single point and the result is
unreadable. The renderer does have rank-packing code for this, but
[gates it](https://github.com/1jehuang/mermaid-rs-renderer/blob/master/src/layout/sankey.rs)
behind `has_cycle || weak_component_count > 1` to keep the geometry of
"ordinary connected acyclic Sankey diagrams" unchanged — which is exactly the
shape that suffers. This is still the case on the renderer's `master`, not only
in the pinned release.

**The `sankey` configuration section does nothing.** The renderer's
`src/config.rs` does not mention Sankey at all. The canvas (560–640 × 360), the
node width (10) and the gap are hard-coded, and `showValues`, `nodeAlignment`,
`linkColor`, `nodeWidth`, `nodePadding`, `prefix` and `suffix` are
unimplemented — link values are always drawn, whatever `showValues` says.

**YAML front matter is skipped, not read.** A `---` block is stripped so the
diagram still parses, but neither `title:` nor a `config:` mapping has any
effect; only Gantt's `displayMode` is wired up. The documentation's headline
examples for the class, ER, state, Git, packet, radar and Sankey diagrams all
carry front matter, and none of it is honoured. Pass configuration through
`Options` instead, which does work.

**`%%{init: ...}%%` directives are ignored** in the pinned build. This was
fixed upstream after 0.3.1 ("Apply `%%{init}%%` directives in library API entry
points"), so a future release should bring it.

Each of these is pinned by a test in `sankey_test.go` that asserts the _broken_
behaviour, so that a `mermaid.wasm` upgrade which fixes one fails loudly rather
than passing unnoticed.

If you need pixel parity with upstream Mermaid, this is the wrong tool — use
`@mermaid-js/mermaid-cli` and a headless browser. If you want diagrams from a
Go binary with no external runtime, this is the trade.

## Performance

On an Apple M1:

|                                   |                                      |
| --------------------------------- | ------------------------------------ |
| `New` (cold)                      | ~1.3 s — compiling the 2.9 MB module |
| `New` (shared `CompilationCache`) | ~23 ms                               |
| `Render`, small flowchart         | ~50 ms                               |

The compile dominates, so `New` is the thing to avoid repeating.
`WithCompilationCache` shares compiled machine code between `Renderer`s:

```go
cache := wazero.NewCompilationCache()
defer cache.Close(ctx)

r, err := mermaid.New(ctx, mermaid.WithCompilationCache(cache))
```

`wazero.NewCompilationCacheWithDir` persists that to disk, which pays the
compile once per machine rather than once per process — what a short-lived
command-line tool wants.

For reference, the same render takes ~24 ms under Node and V8, so wazero costs
roughly 2× on the render path and nothing on fidelity.

## Fonts

The renderer measures label text to size nodes. `RegisterFont` (or the
`WithFont` option) supplies a TrueType or OpenType font for that; the first one
registered becomes the fallback for the generic CSS families. Register fonts
before rendering — measurements are cached per family. Without a registered
font the renderer falls back to its own built-in metrics, which is fine for
Latin text and less good for scripts it does not know.

## Concurrency

A `Renderer` is safe for concurrent use but serialises internally — one
WebAssembly instance has one linear memory and is not reentrant. For real
parallelism, give each goroutine its own `Renderer` and share a
`CompilationCache` between them.

## How it fits together

`mermaid-wasm-renderer` is a [wasm-bindgen](https://github.com/rustwasm/wasm-bindgen)
build, which normally needs its generated JavaScript wrapper. Two things make
it usable from Go instead:

**The interface is flat.** `renderSvg` and `renderSvgWithConfig` take strings
as `(ptr, len)` pairs in linear memory and return `(ptr, len, externref,
is_error)`. No JavaScript objects cross the boundary in either direction, so
the wrapper has nothing to translate.

**There are only two imports.** The module asks for a JavaScript `Error`
constructor and an externref-table initialiser. The first is answered by
reading the message out of linear memory — that text becomes the Go error — and
returning a null reference, since there is no JavaScript to throw into. The
second seeds the externref table with `undefined`, `null`, `true` and `false`
for the benefit of glue that is not present here, and wazero does not expose
table mutation to host functions in any case, so it is a no-op; the module
grows the table itself when it needs to.

Two details of the calling convention are easy to get wrong and fail late
rather than loudly:

- The callee takes **ownership** of every string and byte buffer passed in.
  wasm-bindgen's own wrapper allocates these and never frees them. Freeing them
  from the host as well is a double free that corrupts the allocator and
  surfaces much later as an out-of-bounds access during an unrelated render.
  Only the returned SVG is freed by the caller.
- A failed render still allocates an externref table slot for the error, which
  has to be released with `__externref_table_dealloc` or the table grows
  without bound.

Both are covered by tests.

## Upgrading mermaid.wasm

`scripts/fetch-mermaid.sh` resolves a release from the npm registry, downloads
the tarball and copies out the module and a matching `version.go`:

```sh
scripts/fetch-mermaid.sh              # latest release
scripts/fetch-mermaid.sh -v 0.3.1     # a specific one
scripts/fetch-mermaid.sh -n           # report what it would write, change nothing
go test ./...
```

It needs only `curl`, `tar`, `awk` and `python3`. Unlike an Emscripten build
there is no symbol table to regenerate — the Go package addresses exports by
their plain names and matches imports by prefix, so the `__wbg_Error_<hash>`
name changing between builds does not matter. What the script does check is the
shape of that interface: it refuses to write if an expected export is missing
or if the build has grown an import the package does not implement, so an
incompatible release fails at fetch time rather than at the first render.

Currently pinned to **mermaid-wasm-renderer 0.3.1**, which is the latest
published release; the renderer's `master` has moved on but has not been cut.

After an upgrade, re-check the support table in `examples_test.go` and the
Fidelity section above — the tests that pin known defects are written to fail
when the defect is fixed, and each says what to do about it.

## Refreshing the documentation corpus

`scripts/fetch-examples.sh` re-copies `testdata/examples/` from the Mermaid
documentation, one headline example per diagram type:

```sh
scripts/fetch-examples.sh             # from mermaid-js/mermaid@develop
scripts/fetch-examples.sh -r v12.0.0  # from a tag
scripts/fetch-examples.sh -n          # report what would change, write nothing
go test -run TestExample ./...
```

It needs only `curl` and `python3`. Which block of which documentation page
becomes which file is a table inside the script, and needs revisiting when a
page is reorganised — a page whose first example is a bare syntax fragment gets
a later, representative block instead. The support classification is not
fetched: it lives in `examples_test.go` and is this package's own finding.

## Looking at the output

`TestWriteOutput` renders the documentation corpus in every theme into
`testdata/output/`, alongside an `index.html` that shows them side by side,
with the unsupported types called out:

```sh
go test -run TestWriteOutput ./...
open testdata/output/index.html
```

The directory is wiped and rebuilt on each run, and is gitignored. Nothing is
compared against a golden file — it exists so a renderer upgrade can be
eyeballed.

## Licence

This package is MIT licensed. The bundled `mermaid.wasm` is
mermaid-rs-renderer, also MIT — keep `LICENSE.mermaid-rs-renderer` alongside it
when redistributing.
