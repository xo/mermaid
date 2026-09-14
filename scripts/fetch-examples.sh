#!/usr/bin/env bash
#
# Refresh testdata/examples from the Mermaid documentation.
#
# The corpus is one diagram per type, copied verbatim from the headline example
# on the corresponding documentation page, so that a renderer upgrade can be
# judged against what Mermaid itself advertises rather than against fragments
# invented here. The pages live in packages/mermaid/src/docs/syntax/ in the
# mermaid-js/mermaid repository; the examples are the fenced mermaid-example
# blocks inside them.
#
# Usage:
#   scripts/fetch-examples.sh [-r REF] [-o DIR] [-n]
#
#   -r REF   git ref in mermaid-js/mermaid to take the docs from (default: develop)
#   -o DIR   examples directory to write into (default: testdata/examples)
#   -n       report what would be written without touching any file
#
# Which block of which page becomes which file is the table below. It needs
# revisiting when the documentation reorganises a page — a page whose headline
# example is a bare syntax fragment gets a later, representative block instead.
#
# The support classification (which of these the bundled renderer actually
# understands) is not fetched: it lives in the examples table in
# examples_test.go and has to be revisited by running the tests.
#
#   scripts/fetch-examples.sh
#   go test -run TestExample ./...

set -euo pipefail

REPO="mermaid-js/mermaid"
REF="develop"
OUT_DIR=""
DRY_RUN=0

while getopts ":r:o:nh" opt; do
  case "$opt" in
    r) REF="$OPTARG" ;;
    o) OUT_DIR="$OPTARG" ;;
    n) DRY_RUN=1 ;;
    h) awk 'NR>1 && /^#/ { sub(/^# ?/, ""); print; next } NR>1 { exit }' "$0"; exit 0 ;;
    \?) echo "unknown option -$OPTARG" >&2; exit 2 ;;
    :)  echo "option -$OPTARG needs an argument" >&2; exit 2 ;;
  esac
done

for cmd in curl python3; do
  command -v "$cmd" >/dev/null || { echo "need $cmd on PATH" >&2; exit 1; }
done

cd "$(dirname "${BASH_SOURCE[0]}")/.."
[ -n "$OUT_DIR" ] || OUT_DIR="testdata/examples"

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

RAW="https://raw.githubusercontent.com/$REPO/$REF/packages/mermaid/src/docs/syntax"

# file stem : docs page : index of the fenced example block on that page
SPEC="
01-flowchart       flowchart                 6
02-swimlanes       swimlanes                 0
03-sequence        sequenceDiagram           0
04-class           classDiagram              0
05-state           stateDiagram              0
06-er              entityRelationshipDiagram 0
07-journey         userJourney               0
08-gantt           gantt                     0
09-pie             pie                       0
10-quadrant        quadrantChart             0
11-requirement     requirementDiagram        0
12-usecase         usecase                   0
13-gitgraph        gitgraph                  0
14-c4              c4                        0
15-mindmap         mindmap                   0
16-timeline        timeline                  0
17-zenuml          zenuml                    0
18-sankey          sankey                    0
19-xychart         xyChart                   0
20-block           block                     0
21-packet          packet                    0
22-kanban          kanban                    2
23-architecture    architecture              0
24-radar           radar                     0
25-eventmodeling   eventmodeling             3
26-treemap         treemap                   0
27-venn            venn                      0
28-ishikawa        ishikawa                  0
29-wardley         wardley                   0
30-cynefin         cynefin                   0
31-treeview        treeView                  0
32-other-pie       examples                  0
32-other-sequence  examples                  2
32-other-graph     examples                  3
32-other-gitgraph  examples                  8
"

sha="$(curl -fsSL "https://api.github.com/repos/$REPO/commits/$REF" \
  | python3 -c 'import json,sys; print(json.load(sys.stdin)["sha"])')" \
  || { echo "could not resolve $REPO@$REF" >&2; exit 1; }
echo "$REPO@$REF is $sha" >&2

pages="$(echo "$SPEC" | awk 'NF {print $2}' | sort -u)"
for page in $pages; do
  curl -fsSL --retry 3 --retry-delay 1 -o "$tmp/$page.md" "$RAW/$page.md" \
    || { echo "no such documentation page: $page.md" >&2; exit 1; }
done

written=0
while read -r stem page idx; do
  [ -n "${stem:-}" ] || continue
  python3 - "$tmp/$page.md" "$idx" > "$tmp/$stem.mmd" <<'PY' || exit 1
import re, sys
src = open(sys.argv[1]).read()
idx = int(sys.argv[2])
blocks = re.findall(r'^```(?:mermaid-example|mermaid|zenuml)\n(.*?)^```', src, re.M | re.S)
if idx >= len(blocks):
    sys.exit(f'{sys.argv[1]}: wanted block {idx}, the page has {len(blocks)}')
sys.stdout.write(blocks[idx].strip() + '\n')
PY
  if [ "$DRY_RUN" -eq 1 ]; then
    if [ -f "$OUT_DIR/$stem.mmd" ] && cmp -s "$tmp/$stem.mmd" "$OUT_DIR/$stem.mmd"; then
      printf '  %-20s unchanged\n' "$stem.mmd" >&2
    else
      printf '  %-20s would change (syntax/%s.md block %s)\n' "$stem.mmd" "$page" "$idx" >&2
    fi
  else
    cp "$tmp/$stem.mmd" "$OUT_DIR/$stem.mmd"
    chmod 644 "$OUT_DIR/$stem.mmd"
  fi
  written=$((written + 1))
done <<< "$(echo "$SPEC" | awk 'NF {print $1, $2, $3}')"

echo >&2
if [ "$DRY_RUN" -eq 1 ]; then
  echo "dry run: $written examples from $REPO@$sha" >&2
  exit 0
fi
echo "wrote $written examples into $OUT_DIR from $REPO@$sha" >&2
echo >&2
echo "next: go test -run TestExample ./..." >&2
