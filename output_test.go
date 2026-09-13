package mermaid

import (
	"context"
	"fmt"
	"html"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// TestWriteOutput renders every diagram in the test corpus, in every theme,
// into testdata/output/ alongside an index.html that shows them side by side.
//
// Nothing is compared against a golden file — this exists so that a change of
// renderer version can be eyeballed:
//
//	go test -run TestWriteOutput ./...
//	open testdata/output/index.html
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

	names := make([]string, 0, len(diagrams))
	for name := range diagrams {
		names = append(names, name)
	}
	sort.Strings(names)

	var body strings.Builder
	for _, name := range names {
		fmt.Fprintf(&body, "<h2>%s</h2>\n<pre>%s</pre>\n<div class=row>\n",
			html.EscapeString(name), html.EscapeString(diagrams[name]))
		for _, th := range themes {
			svg, err := r.RenderWith(ctx, diagrams[name], Options{Theme: th})
			if err != nil {
				t.Errorf("%s/%s: %v", name, th, err)
				continue
			}
			file := fmt.Sprintf("%s-%s.svg", name, th)
			if err := os.WriteFile(filepath.Join(dir, file), []byte(svg), 0o644); err != nil {
				t.Fatal(err)
			}
			fmt.Fprintf(&body, "<figure><figcaption>%s</figcaption>%s</figure>\n", th, svg)
		}
		body.WriteString("</div>\n")
	}

	page := `<!doctype html>
<meta charset="utf-8">
<title>mermaid render output</title>
<style>
  body { font: 14px/1.5 system-ui, sans-serif; margin: 2rem; background: #fafafa; color: #111; }
  h2 { margin: 2rem 0 .5rem; }
  pre { background: #f0f0f0; padding: .75rem; border-radius: 4px; overflow-x: auto; font-size: 12px; }
  .row { display: flex; flex-wrap: wrap; gap: 1rem; align-items: flex-start; }
  figure { margin: 0; padding: .5rem; background: #fff; border: 1px solid #ddd; border-radius: 6px; }
  figcaption { font-size: 12px; color: #666; margin-bottom: .4rem; }
  svg { max-width: 420px; height: auto; display: block; }
</style>
<h1>mermaid render output</h1>
<p>mermaid-wasm-renderer ` + html.EscapeString(r.Version()) + `</p>
` + body.String()

	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte(page), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Logf("wrote %s/index.html", dir)
}
