package mermaid

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// support records what the pinned renderer does with a diagram type.
type support int

const (
	// supported: mermaid-rs-renderer has a parser and a layout engine for the
	// diagram's header keyword.
	supported support = iota

	// fallback: the renderer does not recognise the header, and its parser
	// falls through to flowchart syntax rather than reporting an error. The
	// result is a well-formed SVG full of boxes labelled with fragments of the
	// source — a silently wrong answer. See TestExamplesFallBackToFlowchart.
	fallback

	// rejected: the renderer reports an error for the header.
	rejected
)

// example is one diagram from the Mermaid documentation, together with where
// it came from and what this renderer makes of it.
type example struct {
	file    string // basename in testdata/examples
	kind    string // the name the Mermaid documentation gives the diagram type
	page    string // packages/mermaid/src/docs/syntax/<page>.md
	support support
}

// examples is the documentation corpus, in the order the Mermaid docs list the
// diagram types. Every file in testdata/examples must appear here and vice
// versa — TestExampleManifest checks that.
//
// scripts/fetch-examples.sh refreshes the .mmd files from the Mermaid
// repository; the support column is this package's own finding and has to be
// revisited whenever mermaid.wasm is upgraded.
var examples = []example{
	{"01-flowchart.mmd", "Flowchart", "flowchart", supported},
	{"02-swimlanes.mmd", "Swimlanes Diagram", "swimlanes", fallback},
	{"03-sequence.mmd", "Sequence Diagram", "sequenceDiagram", supported},
	{"04-class.mmd", "Class Diagram", "classDiagram", supported},
	{"05-state.mmd", "State Diagram", "stateDiagram", supported},
	{"06-er.mmd", "Entity Relationship Diagram", "entityRelationshipDiagram", supported},
	{"07-journey.mmd", "User Journey", "userJourney", supported},
	{"08-gantt.mmd", "Gantt", "gantt", supported},
	{"09-pie.mmd", "Pie Chart", "pie", supported},
	{"10-quadrant.mmd", "Quadrant Chart", "quadrantChart", supported},
	{"11-requirement.mmd", "Requirement Diagram", "requirementDiagram", supported},
	{"12-usecase.mmd", "Use Case Diagram", "usecase", fallback},
	{"13-gitgraph.mmd", "GitGraph (Git) Diagram", "gitgraph", supported},
	{"14-c4.mmd", "C4 Diagram", "c4", supported},
	{"15-mindmap.mmd", "Mindmaps", "mindmap", supported},
	{"16-timeline.mmd", "Timeline", "timeline", supported},
	{"17-zenuml.mmd", "ZenUML", "zenuml", supported},
	{"18-sankey.mmd", "Sankey", "sankey", supported},
	{"19-xychart.mmd", "XY Chart", "xyChart", supported},
	{"20-block.mmd", "Block Diagram", "block", supported},
	{"21-packet.mmd", "Packet", "packet", supported},
	{"22-kanban.mmd", "Kanban", "kanban", supported},
	{"23-architecture.mmd", "Architecture", "architecture", supported},
	{"24-radar.mmd", "Radar", "radar", supported},
	{"25-eventmodeling.mmd", "Event Modeling", "eventmodeling", rejected},
	{"26-treemap.mmd", "Treemap", "treemap", supported},
	{"27-venn.mmd", "Venn", "venn", fallback},
	{"28-ishikawa.mmd", "Ishikawa", "ishikawa", fallback},
	{"29-wardley.mmd", "Wardley", "wardley", fallback},
	{"30-cynefin.mmd", "Cynefin", "cynefin", fallback},
	{"31-treeview.mmd", "TreeView", "treeView", fallback},

	// "Other Examples" is a gallery page rather than a diagram type.
	{"32-other-pie.mmd", "Other Examples: pie", "examples", supported},
	{"32-other-sequence.mmd", "Other Examples: sequence", "examples", supported},
	{"32-other-graph.mmd", "Other Examples: graph", "examples", supported},
	{"32-other-gitgraph.mmd", "Other Examples: gitGraph", "examples", supported},
}

const examplesDir = "testdata/examples"

func (e example) source(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(examplesDir, e.file))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// header is the diagram's first word — "venn" for "venn-beta", "flowchart" for
// "flowchart LR". Unsupported types leak it into the fallback flowchart as a
// node label, which is how TestExamplesFallBackToFlowchart recognises one.
func (e example) header(t *testing.T) string {
	t.Helper()
	for _, line := range strings.Split(e.source(t), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || line == "---" || strings.HasPrefix(line, "%%") {
			continue
		}
		// Skip a YAML front-matter block.
		if strings.Contains(line, ":") && !strings.ContainsAny(line, "-[(") {
			continue
		}
		return strings.FieldsFunc(line, func(r rune) bool {
			return r == ' ' || r == '-' || r == '\t'
		})[0]
	}
	t.Fatalf("%s: no diagram header", e.file)
	return ""
}

var (
	reTextElem = regexp.MustCompile(`(?s)<text[^>]*>(.*?)</text>`)
	reAnyTag   = regexp.MustCompile(`<[^>]*>`)
)

// svgLabels returns the rendered text of every <text> element, with any nested
// markup (tspan and friends) flattened away.
func svgLabels(svg string) []string {
	var out []string
	for _, m := range reTextElem.FindAllStringSubmatch(svg, -1) {
		out = append(out, strings.TrimSpace(reAnyTag.ReplaceAllString(m[1], "")))
	}
	return out
}

// TestExampleManifest keeps the examples table and testdata/examples in step,
// so that adding a file without classifying it, or classifying a file that is
// not there, is caught rather than silently ignored.
func TestExampleManifest(t *testing.T) {
	onDisk := map[string]bool{}
	files, err := filepath.Glob(filepath.Join(examplesDir, "*.mmd"))
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		onDisk[filepath.Base(f)] = true
	}

	listed := map[string]bool{}
	for _, e := range examples {
		if listed[e.file] {
			t.Errorf("%s is listed twice", e.file)
		}
		listed[e.file] = true
		if !onDisk[e.file] {
			t.Errorf("%s is in the table but not in %s", e.file, examplesDir)
		}
	}
	for f := range onDisk {
		if !listed[f] {
			t.Errorf("%s is in %s but not in the table", f, examplesDir)
		}
	}

	// The README quotes these counts; -v prints them so they can be checked.
	// The "Other Examples" rows are a gallery of types already counted, so
	// they are left out.
	var n [3]int
	types := 0
	for _, e := range examples {
		if e.page == "examples" {
			continue
		}
		n[e.support]++
		types++
	}
	t.Logf("of %d documented diagram types: %d supported, %d parsed as a flowchart, %d rejected",
		types, n[supported], n[fallback], n[rejected])
}

// TestExamplesRender renders every documentation example the renderer claims
// to support and checks the result is a well-formed SVG that carries the
// diagram's own text.
func TestExamplesRender(t *testing.T) {
	ctx := context.Background()
	r := newTestRenderer(t)
	for _, e := range examples {
		if e.support != supported {
			continue
		}
		t.Run(e.file, func(t *testing.T) {
			svg, err := r.Render(ctx, e.source(t))
			if err != nil {
				t.Fatalf("%s (%s): %v", e.kind, e.file, err)
			}
			wellFormed(t, e.file, svg)
			if len(svgLabels(svg)) == 0 {
				t.Errorf("%s: rendered without a single text label", e.kind)
			}
		})
	}
}

// TestExamplesFallBackToFlowchart pins the renderer's worst failure mode: for a
// diagram type it does not implement, its parser falls through to flowchart
// syntax instead of reporting an error, and the header keyword itself surfaces
// as a node label — "venn" and "beta" end up in boxes.
//
// The assertion is deliberately backwards: it holds only while the type is
// unimplemented. When an upgrade adds real support the test fails, which is the
// signal to move that row of the examples table to `supported`.
func TestExamplesFallBackToFlowchart(t *testing.T) {
	ctx := context.Background()
	r := newTestRenderer(t)
	for _, e := range examples {
		if e.support != fallback {
			continue
		}
		t.Run(e.file, func(t *testing.T) {
			svg, err := r.Render(ctx, e.source(t))
			if err != nil {
				t.Fatalf("%s: expected a flowchart fallback, got an error: %v", e.kind, err)
			}
			wellFormed(t, e.file, svg)

			want := e.header(t)
			for _, label := range svgLabels(svg) {
				if label == want {
					return
				}
			}
			t.Errorf("%s: %q no longer leaks into a node label — the renderer may have "+
				"gained real support; reclassify this example as supported",
				e.kind, want)
		})
	}
}

// TestExamplesRejected covers the diagram types the renderer turns away
// outright. Unlike the fallback types these at least fail loudly.
func TestExamplesRejected(t *testing.T) {
	ctx := context.Background()
	r := newTestRenderer(t)
	for _, e := range examples {
		if e.support != rejected {
			continue
		}
		t.Run(e.file, func(t *testing.T) {
			if _, err := r.Render(ctx, e.source(t)); err == nil {
				t.Errorf("%s: expected an error, got a render — reclassify this example",
					e.kind)
			}
		})
	}
}
