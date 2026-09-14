package mermaid

import (
	"context"
	"encoding/xml"
	"strings"
	"sync"
	"testing"

	"github.com/tetratelabs/wazero"
)

// diagrams is one small, valid source per supported diagram family, kept
// minimal so the breadth tests stay fast. The gallery and the conformance
// checks use the documentation corpus in testdata/examples instead; see
// examples_test.go.
var diagrams = map[string]string{
	"flowchart": "flowchart TD\n" +
		"  A[Start] --> B{Is it working?}\n" +
		"  B -->|Yes| C[Great success]\n" +
		"  B -->|No| D[Debug it]\n" +
		"  D --> B",
	"sequence": "sequenceDiagram\n" +
		"  Alice->>John: Hello John, how are you?\n" +
		"  John-->>Alice: Great!\n" +
		"  Alice-)John: See you later!",
	"class": "classDiagram\n" +
		"  Animal <|-- Duck\n" +
		"  Animal : +int age\n" +
		"  Animal : +String gender\n" +
		"  class Duck{\n" +
		"    +String beakColor\n" +
		"    +swim()\n" +
		"  }",
	"state": "stateDiagram-v2\n" +
		"  [*] --> Still\n" +
		"  Still --> [*]\n" +
		"  Still --> Moving\n" +
		"  Moving --> Still\n" +
		"  Moving --> Crash\n" +
		"  Crash --> [*]",
	"pie": "pie title Pets adopted by volunteers\n" +
		"  \"Dogs\" : 386\n" +
		"  \"Cats\" : 85\n" +
		"  \"Rats\" : 15",
	"er": "erDiagram\n" +
		"  CUSTOMER ||--o{ ORDER : places\n" +
		"  ORDER ||--|{ LINE-ITEM : contains",
	"gantt": "gantt\n" +
		"  title A Gantt Diagram\n" +
		"  dateFormat YYYY-MM-DD\n" +
		"  section Section\n" +
		"  A task :a1, 2014-01-01, 30d\n" +
		"  Another :after a1, 20d",
	"journey": "journey\n" +
		"  title My working day\n" +
		"  section Go to work\n" +
		"    Make tea: 5: Me\n" +
		"    Go upstairs: 3: Me",
	"mindmap": "mindmap\n" +
		"  root((mindmap))\n" +
		"    Origins\n" +
		"      Long history\n" +
		"    Research",
}

func newTestRenderer(t *testing.T) *Renderer {
	t.Helper()
	r, err := New(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { r.Close(context.Background()) })
	return r
}

// wellFormed checks that s parses as XML and looks like an SVG document, so a
// truncated or half-written read cannot pass as a successful render.
func wellFormed(t *testing.T, name, s string) {
	t.Helper()
	if !strings.HasPrefix(s, "<svg") {
		t.Errorf("%s: does not start with <svg: %.60q", name, s)
		return
	}
	if err := xml.Unmarshal([]byte(s), new(struct {
		XMLName xml.Name
	})); err != nil {
		t.Errorf("%s: not well-formed XML: %v", name, err)
	}
}

func TestRenderDiagramTypes(t *testing.T) {
	ctx := context.Background()
	r := newTestRenderer(t)
	for name, src := range diagrams {
		t.Run(name, func(t *testing.T) {
			svg, err := r.Render(ctx, src)
			if err != nil {
				t.Fatal(err)
			}
			wellFormed(t, name, svg)
			if len(svg) < 200 {
				t.Errorf("%s: suspiciously short SVG (%d bytes)", name, len(svg))
			}
		})
	}
}

func TestRenderLabelsSurvive(t *testing.T) {
	ctx := context.Background()
	r := newTestRenderer(t)
	svg, err := r.Render(ctx, "flowchart LR\n  A[Alpha] --> B[Bravo]")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Alpha", "Bravo"} {
		if !strings.Contains(svg, want) {
			t.Errorf("SVG is missing the label %q", want)
		}
	}
}

func TestThemes(t *testing.T) {
	ctx := context.Background()
	r := newTestRenderer(t)
	src := "flowchart TD\n  A[Start] --> B[End]"

	seen := map[string]Theme{}
	for _, th := range []Theme{ThemeDefault, ThemeDark, ThemeForest, ThemeNeutral, ThemeModern} {
		svg, err := r.RenderWith(ctx, src, Options{Theme: th})
		if err != nil {
			t.Fatalf("theme %s: %v", th, err)
		}
		wellFormed(t, string(th), svg)
		if prev, ok := seen[svg]; ok {
			t.Errorf("theme %s rendered identically to %s", th, prev)
		}
		seen[svg] = th
	}
}

func TestUnknownTheme(t *testing.T) {
	ctx := context.Background()
	r := newTestRenderer(t)
	_, err := r.RenderWith(ctx, "flowchart TD\n A-->B", Options{Theme: "chartreuse"})
	if err == nil {
		t.Fatal("want an error for an unknown theme, got nil")
	}
	if !strings.Contains(err.Error(), "chartreuse") {
		t.Errorf("error should name the bad theme, got %v", err)
	}
}

func TestConfig(t *testing.T) {
	ctx := context.Background()
	r := newTestRenderer(t)
	src := "flowchart TD\n  A[Start] --> B[End]"

	plain, err := r.Render(ctx, src)
	if err != nil {
		t.Fatal(err)
	}
	themed, err := r.RenderWith(ctx, src, Options{Config: []byte(`{"theme":"dark"}`)})
	if err != nil {
		t.Fatal(err)
	}
	wellFormed(t, "config", themed)
	if plain == themed {
		t.Error("Config {\"theme\":\"dark\"} changed nothing")
	}

	// Theme wins over the config's own theme key.
	forced, err := r.RenderWith(ctx, src, Options{Theme: ThemeForest, Config: []byte(`{"theme":"dark"}`)})
	if err != nil {
		t.Fatal(err)
	}
	if forced == themed {
		t.Error("Options.Theme should take precedence over Config's theme")
	}
}

func TestInvalidConfigJSON(t *testing.T) {
	ctx := context.Background()
	r := newTestRenderer(t)
	if _, err := r.RenderWith(ctx, "flowchart TD\n A-->B", Options{Config: []byte("{bad")}); err == nil {
		t.Fatal("want an error for malformed Config JSON, got nil")
	}
}

func TestSyntaxError(t *testing.T) {
	ctx := context.Background()
	r := newTestRenderer(t)
	_, err := r.Render(ctx, "not a diagram at all")
	if err == nil {
		t.Fatal("want an error for invalid source, got nil")
	}
	// The renderer's own diagnostic should reach the caller intact.
	if !strings.Contains(err.Error(), "1:1") {
		t.Errorf("error should carry the renderer's position info, got %v", err)
	}
}

// A failed render must not poison the instance: the error path allocates an
// externref slot, and if it were not released the table would grow without
// bound.
func TestRecoversAfterError(t *testing.T) {
	ctx := context.Background()
	r := newTestRenderer(t)
	for i := 0; i < 50; i++ {
		if _, err := r.Render(ctx, "definitely not a diagram"); err == nil {
			t.Fatalf("iteration %d: want an error", i)
		}
		if _, err := r.Render(ctx, "flowchart TD\n A-->B"); err != nil {
			t.Fatalf("iteration %d: %v", i, err)
		}
	}
}

func TestEmptySource(t *testing.T) {
	ctx := context.Background()
	r := newTestRenderer(t)
	if _, err := r.Render(ctx, ""); err == nil {
		t.Fatal("want an error for empty source, got nil")
	}
}

func TestRenderAfterClose(t *testing.T) {
	ctx := context.Background()
	r, err := New(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := r.Close(ctx); err != nil {
		t.Fatal(err)
	}
	if err := r.Close(ctx); err != nil {
		t.Errorf("second Close should be a no-op, got %v", err)
	}
	if _, err := r.Render(ctx, "flowchart TD\n A-->B"); err == nil {
		t.Fatal("want an error rendering on a closed renderer, got nil")
	}
}

func TestConcurrentRender(t *testing.T) {
	ctx := context.Background()
	r := newTestRenderer(t)

	want, err := r.Render(ctx, diagrams["flowchart"])
	if err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup
	errs := make(chan error, 32)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 4; j++ {
				got, err := r.Render(ctx, diagrams["flowchart"])
				if err != nil {
					errs <- err
					return
				}
				if got != want {
					errs <- errDiffer
					return
				}
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
}

var errDiffer = errConst("concurrent render produced a different SVG")

type errConst string

func (e errConst) Error() string { return string(e) }

func TestUnicodeLabels(t *testing.T) {
	ctx := context.Background()
	r := newTestRenderer(t)
	// Multi-byte text exercises the UTF-8 copy into linear memory, where a
	// length measured in runes rather than bytes would truncate the source.
	svg, err := r.Render(ctx, "flowchart LR\n  A[日本語のラベル] --> B[Ünïcödé]")
	if err != nil {
		t.Fatal(err)
	}
	wellFormed(t, "unicode", svg)
	if !strings.Contains(svg, "日本語のラベル") {
		t.Error("SVG is missing the Japanese label")
	}
}

func TestOneShotRender(t *testing.T) {
	svg, err := Render(context.Background(), diagrams["pie"])
	if err != nil {
		t.Fatal(err)
	}
	wellFormed(t, "oneshot", svg)
}

func TestVersion(t *testing.T) {
	r := newTestRenderer(t)
	if r.Version() == "" {
		t.Error("Version is empty")
	}
}

func BenchmarkRender(b *testing.B) {
	ctx := context.Background()
	r, err := New(ctx)
	if err != nil {
		b.Fatal(err)
	}
	defer r.Close(ctx)
	src := diagrams["flowchart"]

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := r.Render(ctx, src); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkNew(b *testing.B) {
	ctx := context.Background()
	for i := 0; i < b.N; i++ {
		r, err := New(ctx)
		if err != nil {
			b.Fatal(err)
		}
		r.Close(ctx)
	}
}

// The input strings are handed to Rust and freed there. If that assumption is
// wrong the allocator never reclaims them, so linear memory grows without
// bound; this catches that without depending on an exact footprint.
func TestNoMemoryGrowth(t *testing.T) {
	ctx := context.Background()
	r := newTestRenderer(t)
	src := diagrams["flowchart"]

	if testing.Short() {
		t.Skip("renders hundreds of diagrams")
	}
	for i := 0; i < 20; i++ {
		if _, err := r.Render(ctx, src); err != nil {
			t.Fatal(err)
		}
	}
	settled := r.mod.Memory().Size()

	for i := 0; i < 300; i++ {
		if _, err := r.Render(ctx, src); err != nil {
			t.Fatal(err)
		}
	}
	if grown := r.mod.Memory().Size(); grown > settled {
		t.Errorf("linear memory grew from %d to %d bytes over 300 renders", settled, grown)
	}
}

func TestCompilationCacheIsShared(t *testing.T) {
	ctx := context.Background()
	cache := wazero.NewCompilationCache()
	defer cache.Close(ctx)

	for i := 0; i < 2; i++ {
		r, err := New(ctx, WithCompilationCache(cache))
		if err != nil {
			t.Fatal(err)
		}
		svg, err := r.Render(ctx, "flowchart TD\n A-->B")
		if err != nil {
			t.Fatal(err)
		}
		wellFormed(t, "cached", svg)
		r.Close(ctx)
	}
}

// moreDiagrams covers the rest of the renderer's repertoire, including the
// newer beta diagram types. These are breadth checks rather than gallery
// material, so they are kept separate from the corpus above.
var moreDiagrams = map[string]string{
	"graph-lr":     "graph LR\n  A --> B",
	"quadrant":     "quadrantChart\n  title Reach and engagement\n  x-axis Low --> High\n  y-axis Low --> High\n  Campaign A: [0.3, 0.6]",
	"requirement":  "requirementDiagram\n  requirement R {\n  id: 1\n  text: the thing\n  risk: high\n  verifymethod: test\n  }",
	"gitgraph":     "gitGraph\n  commit\n  branch dev\n  commit",
	"timeline":     "timeline\n  title History\n  2021 : Alpha\n  2022 : Beta",
	"sankey":       "sankey-beta\nA,B,10\nB,C,5",
	"xychart":      "xychart-beta\n  title \"Sales\"\n  x-axis [jan, feb, mar]\n  y-axis \"Revenue\" 0 --> 100\n  bar [10, 50, 90]",
	"block":        "block-beta\n  columns 2\n  A B",
	"packet":       "packet-beta\n  0-7: \"Source\"\n  8-15: \"Dest\"",
	"kanban":       "kanban\n  Todo\n    [Task one]\n  Done\n    [Task two]",
	"architecture": "architecture-beta\n  group api(cloud)[API]\n  service db(database)[DB] in api",
	"c4":           "C4Context\n  title System Context\n  Person(a, \"Customer\", \"a user\")",
	"radar":        "radar-beta\n  axis speed, power, range\n  curve car{80, 60, 40}",
	"treemap":      "treemap-beta\n\"Root\"\n  \"A\": 10\n  \"B\": 20",
	"zenuml":       "zenuml\n  A->B: hello",
}

func TestRenderMoreDiagramTypes(t *testing.T) {
	ctx := context.Background()
	r := newTestRenderer(t)
	for name, src := range moreDiagrams {
		t.Run(name, func(t *testing.T) {
			svg, err := r.Render(ctx, src)
			if err != nil {
				t.Fatal(err)
			}
			wellFormed(t, name, svg)
		})
	}
}
