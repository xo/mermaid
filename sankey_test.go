package mermaid

import (
	"context"
	"regexp"
	"strconv"
	"testing"
)

// The tests in this file pin defects in the bundled renderer rather than
// correct behaviour. Each asserts the *broken* result, so that a mermaid.wasm
// upgrade which fixes it fails here loudly instead of going unnoticed. Every
// one names what the fix should look like.

// reSankeyNode picks the position and height out of a rendered Sankey node.
// The renderer emits <g class="node" id="node-N" transform="translate(x,y)">
// wrapping a <rect height=... width=.../>.
var reSankeyNode = regexp.MustCompile(
	`<g class="node" id="(node-\d+)" transform="translate\(([\d.-]+),([\d.-]+)\)"[^>]*>` +
		`<rect height="([\d.]+)"`)

type sankeyNode struct {
	id      string
	x, y, h float64
}

func parseSankeyNodes(t *testing.T, svg string) []sankeyNode {
	t.Helper()
	ms := reSankeyNode.FindAllStringSubmatch(svg, -1)
	if len(ms) == 0 {
		t.Fatal("no Sankey nodes in the SVG; the renderer's markup has changed")
	}
	num := func(s string) float64 {
		f, err := strconv.ParseFloat(s, 64)
		if err != nil {
			t.Fatal(err)
		}
		return f
	}
	nodes := make([]sankeyNode, 0, len(ms))
	for _, m := range ms {
		nodes = append(nodes, sankeyNode{m[1], num(m[2]), num(m[3]), num(m[4])})
	}
	return nodes
}

// TestSankeyRanksOverlap records the renderer's central Sankey defect: within a
// column, only nodes that have an incoming link are given a vertical position.
// Every source node therefore sits at y=0, stacked on top of the others, and
// the flows leaving them all start from the same band.
//
// mermaid-rs-renderer does have packing code for this, but master gates it
// behind `requires_compaction = has_cycle || weak_component_count > 1` to keep
// the geometry of "ordinary connected acyclic Sankey diagrams" byte-for-byte —
// which is exactly the shape this diagram, and the one on the Mermaid
// documentation page, happen to be.
//
// The fix upstream is to run the rank packing unconditionally. When that lands,
// this test fails and should be inverted into an assertion that no two nodes in
// a column overlap.
func TestSankeyRanksOverlap(t *testing.T) {
	ctx := context.Background()
	r := newTestRenderer(t)

	// Three sources feeding one sink: one weak component, no cycle.
	svg, err := r.Render(ctx, "sankey-beta\nA,X,10\nB,X,20\nC,X,30\n")
	if err != nil {
		t.Fatal(err)
	}

	nodes := parseSankeyNodes(t, svg)
	if len(nodes) != 4 {
		t.Fatalf("want 4 nodes, got %d", len(nodes))
	}

	var sources []sankeyNode
	for _, n := range nodes {
		if n.x == 0 {
			sources = append(sources, n)
		}
	}
	if len(sources) != 3 {
		t.Fatalf("want 3 nodes in the first column, got %d", len(sources))
	}
	for _, n := range sources {
		if n.y != 0 {
			t.Fatalf("%s is at y=%v: the renderer now spreads a column out, "+
				"so the Sankey layout defect is fixed — invert this test into a "+
				"no-overlap assertion", n.id, n.y)
		}
	}
}

// TestSankeyDocExampleOverlaps is the same defect on the diagram from the
// Mermaid Sankey documentation page, where it is far more damaging: the energy
// flow chart has 48 nodes, and every one without an inbound link lands on y=0.
func TestSankeyDocExampleOverlaps(t *testing.T) {
	ctx := context.Background()
	r := newTestRenderer(t)

	var e example
	for _, x := range examples {
		if x.kind == "Sankey" {
			e = x
		}
	}
	svg, err := r.Render(ctx, e.source(t))
	if err != nil {
		t.Fatal(err)
	}

	// Count nodes sharing a column and a y, which is how the overlap shows up.
	type slot struct{ x, y float64 }
	seen := map[slot]int{}
	worst := 0
	for _, n := range parseSankeyNodes(t, svg) {
		s := slot{n.x, n.y}
		seen[s]++
		if seen[s] > worst {
			worst = seen[s]
		}
	}
	if worst < 2 {
		t.Fatal("no two nodes share a position any more — the Sankey layout " +
			"defect is fixed; replace this test with a real geometry check")
	}
	t.Logf("%d nodes share a single position in the documentation example", worst)
}

// TestSankeyConfigIsIgnored records that the renderer implements no part of
// Mermaid's `sankey` configuration section. Its src/config.rs does not mention
// Sankey at all, and the layout hard-codes the canvas (560–640 × 360), the node
// width (10) and the gap; `showValues`, which the documentation example sets in
// its front matter, is not implemented either, so link values are never drawn.
func TestSankeyConfigIsIgnored(t *testing.T) {
	ctx := context.Background()
	r := newTestRenderer(t)
	src := "sankey-beta\nA,X,10\nB,X,20\nC,X,30\n"

	base, err := r.Render(ctx, src)
	if err != nil {
		t.Fatal(err)
	}
	for _, cfg := range []string{
		`{"sankey":{"showValues":true}}`,
		`{"sankey":{"width":1200,"height":800}}`,
		`{"sankey":{"nodeWidth":40}}`,
		`{"sankey":{"nodePadding":40}}`,
		`{"sankey":{"nodeAlignment":"left"}}`,
		`{"sankey":{"linkColor":"gradient"}}`,
		`{"sankey":{"prefix":"$","suffix":"k"}}`,
	} {
		got, err := r.RenderWith(ctx, src, Options{Config: []byte(cfg)})
		if err != nil {
			t.Errorf("%s: %v", cfg, err)
			continue
		}
		if got != base {
			t.Errorf("%s now changes the output — the renderer has grown Sankey "+
				"configuration support; drop it from this test and cover the "+
				"option properly", cfg)
		}
	}
}

// TestFrontMatterIsIgnored records that YAML front matter is skipped rather
// than read: the renderer strips the block so the diagram still parses, but
// neither `title:` nor a `config:` mapping has any effect. Only Gantt's
// `displayMode` is wired up.
//
// This matters beyond Sankey — the documentation's headline examples for the
// class, ER, state, Git, packet and radar diagrams all carry a `title:` in
// front matter, and none of those titles are drawn.
func TestFrontMatterIsIgnored(t *testing.T) {
	ctx := context.Background()
	r := newTestRenderer(t)
	base := "flowchart TD\n  A[Start] --> B[End]"

	plain, err := r.Render(ctx, base)
	if err != nil {
		t.Fatal(err)
	}
	for name, src := range map[string]string{
		"title":  "---\ntitle: Hello Title\n---\n" + base,
		"config": "---\nconfig:\n  theme: dark\n---\n" + base,
	} {
		got, err := r.Render(ctx, src)
		if err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		if got != plain {
			t.Errorf("front-matter %s now changes the output — the renderer reads "+
				"front matter; cover it properly instead", name)
		}
	}
}

// TestInitDirectiveIsIgnored records that a %%{init: ...}%% directive has no
// effect in this build. mermaid-rs-renderer fixed this after the 0.3.1 release
// ("Apply %%{init}%% directives in library API entry points", 2026-07-09), so
// the next published release should make this test fail.
func TestInitDirectiveIsIgnored(t *testing.T) {
	ctx := context.Background()
	r := newTestRenderer(t)
	base := "flowchart TD\n  A[Start] --> B[End]"

	plain, err := r.Render(ctx, base)
	if err != nil {
		t.Fatal(err)
	}
	got, err := r.Render(ctx, `%%{init: {"theme":"dark"}}%%`+"\n"+base)
	if err != nil {
		t.Fatal(err)
	}
	if got != plain {
		t.Error("%%{init}%% now changes the output — the directive is honoured; " +
			"document it and cover it properly")
	}
}
