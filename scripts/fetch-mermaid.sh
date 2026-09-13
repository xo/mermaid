#!/usr/bin/env bash
#
# Fetch a published mermaid-wasm-renderer release and refresh the bundled
# module: mermaid.wasm plus a matching version.go.
#
# The npm package is a wasm-bindgen build of the mermaid-rs-renderer crate. It
# ships the WebAssembly module next to a small JavaScript wrapper; only the
# module is needed here, because the Go package implements the two imports the
# wrapper would otherwise provide.
#
# Usage:
#   scripts/fetch-mermaid.sh [-v VERSION] [-o DIR] [-n]
#
#   -v VERSION  release to fetch, or "latest" (default: latest)
#   -o DIR      package directory to write into (default: the repo root)
#   -n          report what would be written without touching any file
#
# Run the tests afterwards:
#
#   scripts/fetch-mermaid.sh
#   go test ./...
#
# The source can be overridden for testing:
#   MERMAID_REGISTRY  base registry URL (default: https://registry.npmjs.org)
#   MERMAID_TARBALL   path to a local .tgz to use instead of downloading

set -euo pipefail

REGISTRY="${MERMAID_REGISTRY:-https://registry.npmjs.org}"
PKG="mermaid-wasm-renderer"
VERSION="latest"
OUT_DIR=""
DRY_RUN=0

while getopts ":v:o:nh" opt; do
  case "$opt" in
    v) VERSION="$OPTARG" ;;
    o) OUT_DIR="$OPTARG" ;;
    n) DRY_RUN=1 ;;
    h) awk 'NR>1 && /^#/ { sub(/^# ?/, ""); print; next } NR>1 { exit }' "$0"; exit 0 ;;
    \?) echo "unknown option -$OPTARG" >&2; exit 2 ;;
    :)  echo "option -$OPTARG needs an argument" >&2; exit 2 ;;
  esac
done

for cmd in curl tar awk python3; do
  command -v "$cmd" >/dev/null || { echo "need $cmd on PATH" >&2; exit 1; }
done

# Repo root, so the script works from anywhere.
cd "$(dirname "${BASH_SOURCE[0]}")/.."
[ -n "$OUT_DIR" ] || OUT_DIR="$PWD"
[ -d "$OUT_DIR" ] || { echo "no such directory: $OUT_DIR" >&2; exit 1; }

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

# ---------------------------------------------------------------- download ---

if [ -n "${MERMAID_TARBALL:-}" ]; then
  [ -f "$MERMAID_TARBALL" ] || { echo "no such tarball: $MERMAID_TARBALL" >&2; exit 1; }
  cp "$MERMAID_TARBALL" "$tmp/pkg.tgz"
  echo "using local tarball $MERMAID_TARBALL" >&2
else
  echo "resolving $PKG@$VERSION" >&2
  curl -fsSL --retry 3 --retry-delay 1 -o "$tmp/manifest.json" "$REGISTRY/$PKG/$VERSION" \
    || { echo "no such release: $PKG@$VERSION" >&2; exit 1; }

  tarball="$(grep -o '"tarball":"[^"]*"' "$tmp/manifest.json" | head -1 | sed 's/^"tarball":"//; s/"$//')"
  [ -n "$tarball" ] || { echo "no tarball URL in the registry manifest" >&2; exit 1; }

  echo "downloading $tarball" >&2
  curl -fsSL --retry 3 --retry-delay 1 -o "$tmp/pkg.tgz" "$tarball"
fi

mkdir -p "$tmp/x"
tar xzf "$tmp/pkg.tgz" -C "$tmp/x"

wasm="$tmp/x/package/mermaid_wasm_renderer_bg.wasm"
manifest="$tmp/x/package/package.json"
for f in "$wasm" "$manifest"; do
  [ -f "$f" ] || { echo "the package has no ${f#$tmp/x/package/}; its layout has changed" >&2; exit 1; }
done

version="$(grep -o '"version"[[:space:]]*:[[:space:]]*"[0-9][^"]*"' "$manifest" \
  | head -1 | sed 's/.*"\([0-9][^"]*\)"$/\1/')"
case "$version" in
  [0-9]*) ;;
  *) echo "could not determine the package version" >&2; exit 1 ;;
esac

# ------------------------------------------------------------------ verify ---
#
# The Go package addresses the module by plain export name and implements its
# imports by prefix, so there is no symbol table to regenerate. What does have
# to hold is the shape of that interface -- a build that renamed renderSvg, or
# grew an import that needs real behaviour, must fail here rather than at the
# first render.

python3 - "$wasm" <<'PY' || exit 1
import sys

def uleb(d, p):
    r = s = 0
    while True:
        b = d[p]; p += 1
        r |= (b & 0x7f) << s
        if not b & 0x80:
            return r, p
        s += 7

d = open(sys.argv[1], 'rb').read()
if d[:4] != b'\x00asm':
    sys.exit('not a WebAssembly binary')

p, imports, exports = 8, [], []
while p < len(d):
    sid = d[p]; p += 1
    size, p = uleb(d, p)
    end = p + size
    if sid == 2:
        n, p = uleb(d, p)
        for _ in range(n):
            l, p = uleb(d, p); p += l           # module
            l, p = uleb(d, p); nm = d[p:p+l].decode(); p += l
            k = d[p]; p += 1
            if k == 0:
                _, p = uleb(d, p)
            elif k == 1:
                p += 1; lim = d[p]; p += 1; _, p = uleb(d, p)
                if lim: _, p = uleb(d, p)
            elif k == 2:
                lim = d[p]; p += 1; _, p = uleb(d, p)
                if lim & 1: _, p = uleb(d, p)
            elif k == 3:
                p += 2
            imports.append(nm)
    elif sid == 7:
        n, p = uleb(d, p)
        for _ in range(n):
            l, p = uleb(d, p); nm = d[p:p+l].decode(); p += l
            p += 1
            _, p = uleb(d, p)
            exports.append(nm)
    p = end

need = ['renderSvg', 'renderSvgWithConfig', 'registerFont', 'memory',
        '__wbindgen_malloc', '__wbindgen_free', '__wbindgen_start',
        '__externref_table_dealloc']
missing = [n for n in need if n not in exports]
if missing:
    sys.exit('the build is missing expected exports: ' + ', '.join(missing))

# Every import must be one the Go package answers: the Error constructor
# (whose name carries a per-build hash) or the externref table initialiser.
unknown = [n for n in imports
           if not n.startswith('__wbg_Error_') and n != '__wbindgen_init_externref_table']
if unknown:
    sys.exit('the build needs imports this package does not implement: ' + ', '.join(unknown))
if not any(n.startswith('__wbg_Error_') for n in imports):
    sys.exit('the build has no __wbg_Error_ import; error reporting would be lost')

print(f'verified: {len(exports)} exports, {len(imports)} imports', file=sys.stderr)
PY

# ---------------------------------------------------------------- generate ---

cat > "$tmp/version.go" <<EOF
// Code generated by scripts/fetch-mermaid.sh. DO NOT EDIT.

package mermaid

// wasmPackageVersion is the mermaid-wasm-renderer release the bundled
// mermaid.wasm was taken from.
const wasmPackageVersion = "$version"
EOF

if command -v gofmt >/dev/null; then
  gofmt -w "$tmp/version.go"
fi

if [ "$DRY_RUN" -eq 1 ]; then
  echo >&2
  echo "dry run: would write to $OUT_DIR" >&2
  echo "  mermaid.wasm  $(wc -c < "$wasm" | tr -d ' ') bytes" >&2
  echo "  version.go    $PKG $version" >&2
  exit 0
fi

cp "$wasm" "$OUT_DIR/mermaid.wasm"
cp "$tmp/version.go" "$OUT_DIR/version.go"
chmod 644 "$OUT_DIR/mermaid.wasm" "$OUT_DIR/version.go"

echo >&2
echo "wrote mermaid.wasm and version.go for $PKG $version into $OUT_DIR" >&2
echo >&2
echo "next: go test ./..." >&2
