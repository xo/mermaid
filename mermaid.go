// Package mermaid renders Mermaid diagrams to SVG. Pure Go, no cgo, no
// browser, no Node.
//
// Mermaid itself is a JavaScript library that needs a DOM to lay text out, so
// rendering it normally means driving a headless browser. This package instead
// runs mermaid-rs-renderer — a parser, layout engine and SVG writer written in
// Rust and published as the WebAssembly package mermaid-wasm-renderer — on
// wazero, a pure-Go WebAssembly runtime. Diagram source goes into linear
// memory, an SVG document comes back out.
//
// The bundled module is refreshed by scripts/fetch-mermaid.sh; see version.go
// for the release it came from.
package mermaid

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sync"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
)

//go:embed mermaid.wasm
var mermaidWASM []byte

// Theme names one of the renderer's built-in colour schemes. The zero value
// selects the renderer's own default.
type Theme string

// The built-in themes.
const (
	ThemeDefault Theme = "default"
	ThemeDark    Theme = "dark"
	ThemeForest  Theme = "forest"
	ThemeNeutral Theme = "neutral"
	ThemeModern  Theme = "modern"
)

// Options controls how a diagram is rendered.
type Options struct {
	// Theme selects a built-in colour scheme. It takes precedence over a
	// "theme" key in Config. Empty means the renderer's default.
	Theme Theme

	// Config is a Mermaid configuration document — the same JSON the mmdr CLI
	// takes with --config, with keys such as "themeVariables", "flowchart" and
	// "sequence". Nil means defaults.
	Config json.RawMessage
}

// Renderer owns one WebAssembly instance of the Mermaid renderer.
//
// Creating one costs a WebAssembly compile (roughly a hundred milliseconds for
// the 2.9 MB module), so reuse a Renderer rather than creating one per
// diagram. Methods are safe for concurrent use but serialise internally: the
// instance has a single linear memory and is not reentrant. For real
// parallelism, give each goroutine its own Renderer.
type Renderer struct {
	mu         sync.Mutex
	rt         wazero.Runtime
	mod        api.Module
	fns        map[string]api.Function
	traceStubs io.Writer
	// lastErr is written by the Error host import and read immediately after
	// the call that triggered it, both under mu.
	lastErr string
	closed  bool
}

// Option configures a Renderer.
type Option func(*config)

type config struct {
	wasm       []byte
	traceStubs io.Writer
	fonts      [][]byte
	cache      wazero.CompilationCache
}

// WithWASM supplies an alternative mermaid-wasm-renderer build in place of the
// embedded one.
func WithWASM(b []byte) Option { return func(c *config) { c.wasm = b } }

// WithStubTracing logs calls into unimplemented WebAssembly imports to w. The
// pinned build never reaches one; this is for diagnosing an upgrade.
func WithStubTracing(w io.Writer) Option { return func(c *config) { c.traceStubs = w } }

// WithCompilationCache reuses compiled machine code between Renderers that
// share the cache.
//
// Compiling the 2.9 MB module is by far the expensive part of New — around a
// second, against tens of milliseconds for a render — so a process that builds
// Renderers repeatedly should share one cache between them. A cache created
// with wazero.NewCompilationCacheWithDir persists to disk and so pays that cost
// once per machine rather than once per process, which is what a short-lived
// command-line tool wants.
func WithCompilationCache(c wazero.CompilationCache) Option {
	return func(cfg *config) { cfg.cache = c }
}

// WithFont registers a TrueType or OpenType font for text measurement, as
// RegisterFont does, but during New so the Renderer is ready to use. The first
// font registered becomes the fallback for the generic CSS families.
func WithFont(ttf []byte) Option {
	return func(c *config) { c.fonts = append(c.fonts, ttf) }
}

// New compiles the WebAssembly module and prepares a renderer.
func New(ctx context.Context, opts ...Option) (*Renderer, error) {
	cfg := config{wasm: mermaidWASM}
	for _, o := range opts {
		o(&cfg)
	}

	imports, err := parseImports(cfg.wasm)
	if err != nil {
		return nil, err
	}

	r := &Renderer{traceStubs: cfg.traceStubs, fns: map[string]api.Function{}}
	rtCfg := wazero.NewRuntimeConfig()
	if cfg.cache != nil {
		rtCfg = rtCfg.WithCompilationCache(cfg.cache)
	}
	r.rt = wazero.NewRuntimeWithConfig(ctx, rtCfg)

	if err := r.hostImports(ctx, r.rt, imports); err != nil {
		r.rt.Close(ctx)
		return nil, err
	}

	// Compiling is explicit rather than folded into InstantiateWithConfig so
	// that a shared CompilationCache is actually consulted: with a warm cache
	// this drops from well over a second to tens of milliseconds.
	code, err := r.rt.CompileModule(ctx, cfg.wasm)
	if err != nil {
		r.rt.Close(ctx)
		return nil, fmt.Errorf("mermaid: compile: %w", err)
	}

	// wasm-bindgen modules have no _start; initialisation is __wbindgen_start.
	mod, err := r.rt.InstantiateModule(ctx, code,
		wazero.NewModuleConfig().WithStartFunctions().WithName("mermaid"))
	if err != nil {
		r.rt.Close(ctx)
		return nil, fmt.Errorf("mermaid: instantiate: %w", err)
	}
	r.mod = mod

	if _, err := r.call(ctx, "__wbindgen_start"); err != nil {
		r.rt.Close(ctx)
		return nil, err
	}

	for i, f := range cfg.fonts {
		if err := r.RegisterFont(ctx, f); err != nil {
			r.rt.Close(ctx)
			return nil, fmt.Errorf("mermaid: font %d: %w", i, err)
		}
	}
	return r, nil
}

// Close releases the WebAssembly runtime.
func (r *Renderer) Close(ctx context.Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return nil
	}
	r.closed = true
	return r.rt.Close(ctx)
}

// Version reports the mermaid-wasm-renderer release the bundled module came
// from.
func (r *Renderer) Version() string { return wasmPackageVersion }

// Render renders diagram source to an SVG document using the default theme.
func (r *Renderer) Render(ctx context.Context, src string) (string, error) {
	return r.RenderWith(ctx, src, Options{})
}

// RenderWith renders diagram source to an SVG document.
//
// A syntax error in src is returned as an error carrying the renderer's own
// message, which names the offending token and its line and column.
func (r *Renderer) RenderWith(ctx context.Context, src string, opts Options) (string, error) {
	if src == "" {
		return "", errors.New("mermaid: empty diagram source")
	}
	if opts.Config != nil && !json.Valid(opts.Config) {
		return "", errors.New("mermaid: Options.Config is not valid JSON")
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return "", errors.New("mermaid: renderer is closed")
	}

	// Every string argument is a (ptr, len) pair in linear memory, and the
	// callee takes ownership of the allocation — wasm-bindgen's own glue
	// allocates these and never frees them, because the Rust side does. Freeing
	// them here as well would be a double free, which corrupts the allocator
	// and surfaces much later as an out-of-bounds access on an unrelated
	// render. A nil Option<String> is a null pointer, and its length is then
	// ignored.
	srcPtr, err := r.writeStr(ctx, src)
	if err != nil {
		return "", err
	}

	var cfgPtr, themePtr uint32
	if len(opts.Config) > 0 {
		if cfgPtr, err = r.writeStr(ctx, string(opts.Config)); err != nil {
			return "", err
		}
	}
	if opts.Theme != "" {
		if themePtr, err = r.writeStr(ctx, string(opts.Theme)); err != nil {
			return "", err
		}
	}

	r.lastErr = ""
	out, err := r.callMulti(ctx, "renderSvgWithConfig",
		uint64(srcPtr), uint64(len(src)),
		uint64(cfgPtr), uint64(len(opts.Config)),
		uint64(themePtr), uint64(len(opts.Theme)))
	if err != nil {
		return "", err
	}
	if len(out) != 4 {
		return "", fmt.Errorf("mermaid: renderSvgWithConfig returned %d values, want 4", len(out))
	}

	// (ptr, len, externref index, is_error)
	ptr, n, ref, failed := uint32(out[0]), uint32(out[1]), out[2], out[3] != 0
	if failed {
		// The Error object itself is a null externref — the message was
		// captured on the way past — but its table slot still has to go back.
		r.callLocked(ctx, "__externref_table_dealloc", ref)
		msg := r.lastErr
		if msg == "" {
			msg = "render failed"
		}
		return "", fmt.Errorf("mermaid: %s", msg)
	}
	defer r.callLocked(ctx, "__wbindgen_free", uint64(ptr), uint64(n), 1)

	buf, ok := r.mod.Memory().Read(ptr, n)
	if !ok {
		return "", errors.New("mermaid: could not read the rendered SVG")
	}
	return string(buf), nil
}

// RegisterFont registers a TrueType or OpenType font for text measurement.
//
// The renderer measures label text to size nodes, and the first font
// registered becomes the fallback for the generic CSS families (sans-serif,
// monospace and friends). Register fonts before rendering: measurements are
// cached per family.
func (r *Renderer) RegisterFont(ctx context.Context, ttf []byte) error {
	if len(ttf) == 0 {
		return errors.New("mermaid: empty font data")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return errors.New("mermaid: renderer is closed")
	}

	// As with the string arguments above, registerFont takes ownership of the
	// buffer; it must not be freed here.
	ptr, err := r.mallocLocked(ctx, len(ttf))
	if err != nil {
		return err
	}
	if !r.mod.Memory().Write(ptr, ttf) {
		r.callLocked(ctx, "__wbindgen_free", uint64(ptr), uint64(len(ttf)), 1)
		return errors.New("mermaid: could not write font data")
	}
	_, err = r.callLocked(ctx, "registerFont", uint64(ptr), uint64(len(ttf)))
	return err
}

// writeStr copies s into linear memory. Callers hold r.mu.
func (r *Renderer) writeStr(ctx context.Context, s string) (uint32, error) {
	ptr, err := r.mallocLocked(ctx, len(s))
	if err != nil {
		return 0, err
	}
	if !r.mod.Memory().Write(ptr, []byte(s)) {
		r.callLocked(ctx, "__wbindgen_free", uint64(ptr), uint64(len(s)), 1)
		return 0, errors.New("mermaid: could not write to WebAssembly memory")
	}
	return ptr, nil
}

// mallocLocked allocates n bytes with alignment 1, the alignment wasm-bindgen
// uses for strings and byte slices. Callers hold r.mu.
func (r *Renderer) mallocLocked(ctx context.Context, n int) (uint32, error) {
	p, err := r.callLocked(ctx, "__wbindgen_malloc", uint64(n), 1)
	if err != nil {
		return 0, err
	}
	if p == 0 && n > 0 {
		return 0, errors.New("mermaid: out of WebAssembly memory")
	}
	return uint32(p), nil
}

func (r *Renderer) call(ctx context.Context, name string, args ...uint64) (uint64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.callLocked(ctx, name, args...)
}

func (r *Renderer) callLocked(ctx context.Context, name string, args ...uint64) (uint64, error) {
	out, err := r.callMulti(ctx, name, args...)
	if err != nil || len(out) == 0 {
		return 0, err
	}
	return out[0], nil
}

// callMulti invokes an exported function, which may return several values.
// Callers hold r.mu.
func (r *Renderer) callMulti(ctx context.Context, name string, args ...uint64) ([]uint64, error) {
	fn, ok := r.fns[name]
	if !ok {
		fn = r.mod.ExportedFunction(name)
		if fn == nil {
			return nil, fmt.Errorf("mermaid: export %q is missing; mermaid.wasm is not a mermaid-wasm-renderer build", name)
		}
		r.fns[name] = fn
	}
	out, err := fn.Call(ctx, args...)
	if err != nil {
		return nil, fmt.Errorf("mermaid: %s: %w", name, err)
	}
	return out, nil
}

// Render is the one-shot form: it compiles the module, renders src and tears
// everything down again.
//
// Reach for New and (*Renderer).Render instead when rendering more than one
// diagram — the WebAssembly compile dominates the cost of a single render.
func Render(ctx context.Context, src string) (string, error) {
	return RenderWith(ctx, src, Options{})
}

// RenderWith is the one-shot form of (*Renderer).RenderWith.
func RenderWith(ctx context.Context, src string, opts Options) (string, error) {
	r, err := New(ctx)
	if err != nil {
		return "", err
	}
	defer r.Close(ctx)
	return r.RenderWith(ctx, src, opts)
}
