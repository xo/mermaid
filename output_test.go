package mermaid

import (
	"context"
	"fmt"
	"html"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestWriteOutput renders the documentation corpus in testdata/examples into
// testdata/output/, alongside an index.html that shows every diagram type in
// every theme.
//
// Nothing is compared against a golden file — this exists so that a change of
// renderer version can be eyeballed:
//
//	go test -run TestWriteOutput ./...
//	open testdata/output/index.html
//
// Diagram types the renderer does not implement are rendered once and labelled,
// since all they show is the flowchart the parser falls back to.
//
// The directory is wiped and rebuilt on each run, and is gitignored.
func TestWriteOutput(t *testing.T) {
	ctx := context.Background()
	r := newTestRenderer(t)

	dir := filepath.Join("testdata", "output")
	if err := os.RemoveAll(dir); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}

	themes := []Theme{ThemeDefault, ThemeDark, ThemeForest, ThemeNeutral, ThemeModern}

	var body strings.Builder
	var toc strings.Builder
	for _, e := range examples {
		src := e.source(t)
		anchor := strings.TrimSuffix(e.file, ".mmd")

		note, class := "", "ok"
		switch e.support {
		case fallback:
			note = "not implemented — the parser falls through to flowchart syntax, " +
				"so this is a pile of boxes labelled with the source, not a " +
				html.EscapeString(e.kind)
			class = "warn"
		case rejected:
			note = "not implemented — the renderer rejects the header"
			class = "bad"
		}

		fmt.Fprintf(&toc, `<li class=%s><a href="#%s">%s</a></li>`+"\n",
			class, anchor, html.EscapeString(e.kind))
		fmt.Fprintf(&body, `<section id="%s"><h2 class=%s>%s <small>%s · syntax/%s.md</small></h2>`+"\n",
			anchor, class, html.EscapeString(e.kind), html.EscapeString(e.file), html.EscapeString(e.page))
		if note != "" {
			fmt.Fprintf(&body, "<p class=note>%s</p>\n", note)
		}
		fmt.Fprintf(&body, "<pre>%s</pre>\n<div class=row>\n", html.EscapeString(src))

		// A fallback render looks the same in every theme's worth of wrong, so
		// show one; a rejected one has nothing to show but its error.
		show := themes
		if e.support != supported {
			show = themes[:1]
		}
		for _, th := range show {
			svg, err := r.RenderWith(ctx, src, Options{Theme: th})
			if err != nil {
				if e.support == rejected {
					fmt.Fprintf(&body, "<pre class=err>%s</pre>\n", html.EscapeString(err.Error()))
					continue
				}
				t.Errorf("%s/%s: %v", e.file, th, err)
				continue
			}
			file := fmt.Sprintf("%s-%s.svg", anchor, th)
			if err := os.WriteFile(filepath.Join(dir, file), []byte(svg), 0o644); err != nil {
				t.Fatal(err)
			}
			fmt.Fprintf(&body, "<figure><figcaption>%s</figcaption>%s</figure>\n", th, svg)
		}
		body.WriteString("</div></section>\n")
	}

	page := `<!doctype html>
<meta charset="utf-8">
<title>mermaid render output</title>
<style>
  body { font: 14px/1.5 system-ui, sans-serif; margin: 2rem; background: #fafafa; color: #111; }
  h2 { margin: 2.5rem 0 .5rem; }
  h2 small { font-weight: normal; color: #888; font-size: 12px; }
  h2.warn::after { content: " fallback"; color: #a60; font-size: 12px; }
  h2.bad::after  { content: " unsupported"; color: #a00; font-size: 12px; }
  pre { background: #f0f0f0; padding: .75rem; border-radius: 4px; overflow-x: auto; font-size: 12px; }
  pre.err { background: #fdd; }
  .note { color: #a60; margin: .25rem 0 .5rem; }
  .row { display: flex; flex-wrap: wrap; gap: 1rem; align-items: flex-start; }
  figure { margin: 0; padding: .5rem; background: #fff; border: 1px solid #ddd; border-radius: 6px; }
  figcaption { font-size: 12px; color: #666; margin-bottom: .4rem; }
  svg { max-width: 420px; height: auto; display: block; }
  nav ul { columns: 3; list-style: none; padding: 0; }
  nav li.warn a { color: #a60; }
  nav li.bad a  { color: #a00; }
</style>
<h1>mermaid render output</h1>
<p>mermaid-wasm-renderer ` + html.EscapeString(r.Version()) + `, against the
Mermaid documentation examples in testdata/examples.</p>
<nav><ul>
` + toc.String() + `</ul></nav>
` + body.String()

	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte(page), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Logf("wrote %s/index.html", dir)
}
