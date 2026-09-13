package mermaid

import (
	"context"
	"fmt"
	"strings"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
)

// wasmImport is one entry of the module's import section.
type wasmImport struct {
	Module  string
	Name    string
	Params  []api.ValueType
	Results []api.ValueType
}

// parseImports reads the type and import sections of a WebAssembly binary and
// returns every imported function with its signature.
//
// wasm-bindgen names its JavaScript shims with a hash of the binding's
// signature — the Error constructor is imported as __wbg_Error_92b29b0548f8b746
// — and that hash changes between builds. Reading the import section means the
// host module is built from whatever the binary actually asks for: every
// import is declared with the exact arity it expects, matched by prefix rather
// than by a name that will not survive the next release.
func parseImports(d []byte) ([]wasmImport, error) {
	if len(d) < 8 || string(d[:4]) != "\x00asm" {
		return nil, fmt.Errorf("mermaid: not a WebAssembly binary")
	}
	p := 8
	var err error

	uleb := func() uint32 {
		var r, s uint32
		for {
			if p >= len(d) {
				err = fmt.Errorf("mermaid: truncated wasm binary")
				return 0
			}
			b := d[p]
			p++
			r |= uint32(b&0x7f) << s
			if b&0x80 == 0 {
				return r
			}
			s += 7
		}
	}

	type sig struct{ params, results []api.ValueType }
	var types []sig
	var out []wasmImport

	for p < len(d) && err == nil {
		id := d[p]
		p++
		size := int(uleb())
		if err != nil || p+size > len(d) {
			return nil, fmt.Errorf("mermaid: malformed wasm section")
		}
		end := p + size

		switch id {
		case 1: // type
			for i, n := uint32(0), uleb(); i < n && err == nil; i++ {
				p++ // 0x60 func
				var s sig
				for j, a := uint32(0), uleb(); j < a; j++ {
					s.params = append(s.params, d[p])
					p++
				}
				for j, b := uint32(0), uleb(); j < b; j++ {
					s.results = append(s.results, d[p])
					p++
				}
				types = append(types, s)
			}
		case 2: // import
			for i, n := uint32(0), uleb(); i < n && err == nil; i++ {
				l := uleb()
				mod := string(d[p : p+int(l)])
				p += int(l)
				l = uleb()
				name := string(d[p : p+int(l)])
				p += int(l)
				kind := d[p]
				p++
				switch kind {
				case 0: // func
					ti := uleb()
					if int(ti) >= len(types) {
						return nil, fmt.Errorf("mermaid: import type index out of range")
					}
					out = append(out, wasmImport{mod, name, types[ti].params, types[ti].results})
				case 1: // table
					p++
					lim := d[p]
					p++
					uleb()
					if lim != 0 {
						uleb()
					}
				case 2: // memory
					lim := d[p]
					p++
					uleb()
					if lim&1 != 0 {
						uleb()
					}
				case 3: // global
					p += 2
				default:
					return nil, fmt.Errorf("mermaid: unknown import kind %d", kind)
				}
			}
		}
		p = end
	}
	if err != nil {
		return nil, err
	}
	return out, nil
}

// hostImports registers the module the renderer imports from.
//
// The published build asks for exactly two functions, both from the
// wasm-bindgen glue module: the Error constructor and the externref table
// initialiser. Everything else in the render path is self-contained — there is
// no WASI, no filesystem, no clock and no DOM.
func (r *Renderer) hostImports(ctx context.Context, rt wazero.Runtime, imports []wasmImport) error {
	byModule := map[string][]wasmImport{}
	for _, im := range imports {
		byModule[im.Module] = append(byModule[im.Module], im)
	}
	for modName, ims := range byModule {
		b := rt.NewHostModuleBuilder(modName)
		for _, im := range ims {
			b.NewFunctionBuilder().
				WithGoModuleFunction(r.hostFunc(im), im.Params, im.Results).
				Export(im.Name)
		}
		if _, err := b.Instantiate(ctx); err != nil {
			return fmt.Errorf("mermaid: host module %q: %w", modName, err)
		}
	}
	return nil
}

// hostFunc returns the implementation for one import.
func (r *Renderer) hostFunc(im wasmImport) api.GoModuleFunc {
	switch {
	case strings.HasPrefix(im.Name, "__wbg_Error_"):
		// Rust calls this to build the JavaScript Error it is about to throw,
		// passing the message as a (ptr, len) pair in linear memory. There is
		// no JavaScript here to throw it into, so the message is captured and
		// a null externref handed back: renderSvg reports failure through its
		// fourth return value, and the captured text becomes the Go error.
		return func(ctx context.Context, mod api.Module, stack []uint64) {
			msg, _ := mod.Memory().Read(uint32(stack[0]), uint32(stack[1]))
			r.lastErr = string(msg)
			stack[0] = 0
		}

	case im.Name == "__wbindgen_init_externref_table":
		// In a browser this seeds the externref table with undefined, null,
		// true and false so Rust can return those constants by index. Those
		// four values are only ever read back by JavaScript glue, which is not
		// in play here, and wazero does not expose table mutation to host
		// functions in any case. Leaving the table empty is safe: the only
		// externref this module allocates is the Error above, and
		// __externref_table_alloc grows the table itself when it needs to.
		return func(ctx context.Context, mod api.Module, stack []uint64) {}

	default:
		// Not reached by the pinned build. A future release that adds an
		// import lands here rather than failing to instantiate; WithStubTracing
		// makes that visible.
		n := len(im.Results)
		return func(ctx context.Context, mod api.Module, stack []uint64) {
			if r.traceStubs != nil {
				fmt.Fprintf(r.traceStubs, "mermaid: stub import %s.%s called\n", im.Module, im.Name)
			}
			for i := 0; i < n; i++ {
				stack[i] = 0
			}
		}
	}
}
