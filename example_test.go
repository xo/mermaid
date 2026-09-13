package mermaid_test

import (
	"context"
	"fmt"
	"log"
	"strings"

	"github.com/xo/mermaid"
)

// Rendering a flowchart to SVG.
func Example() {
	ctx := context.Background()

	svg, err := mermaid.Render(ctx, "flowchart TD\n  A[Start] --> B[Done]")
	if err != nil {
		log.Fatal(err)
	}

	fmt.Println(strings.HasPrefix(svg, "<svg"))
	// Output: true
}

// Reusing a Renderer across several diagrams. The WebAssembly compile happens
// once in New, so this is much cheaper than calling Render for each one.
func ExampleRenderer() {
	ctx := context.Background()

	r, err := mermaid.New(ctx)
	if err != nil {
		log.Fatal(err)
	}
	defer r.Close(ctx)

	for _, src := range []string{
		"flowchart LR\n  A --> B",
		"pie title Pets\n  \"Dogs\" : 3\n  \"Cats\" : 1",
	} {
		svg, err := r.Render(ctx, src)
		if err != nil {
			log.Fatal(err)
		}
		fmt.Println(strings.HasPrefix(svg, "<svg"))
	}
	// Output:
	// true
	// true
}

// Choosing a theme and passing Mermaid configuration.
func ExampleRenderer_RenderWith() {
	ctx := context.Background()

	r, err := mermaid.New(ctx)
	if err != nil {
		log.Fatal(err)
	}
	defer r.Close(ctx)

	svg, err := r.RenderWith(ctx, "flowchart TD\n  A --> B", mermaid.Options{
		Theme:  mermaid.ThemeDark,
		Config: []byte(`{"flowchart":{"curve":"linear"}}`),
	})
	if err != nil {
		log.Fatal(err)
	}

	fmt.Println(strings.HasPrefix(svg, "<svg"))
	// Output: true
}

// A syntax error comes back with the renderer's own diagnostic.
func ExampleRenderer_Render_syntaxError() {
	ctx := context.Background()

	_, err := mermaid.Render(ctx, "this is not a diagram")
	fmt.Println(err)
	// Output: mermaid: unexpected token 'this is not a diagram' at 1:1; expected unknown or missing Mermaid diagram header
}
