#!/usr/bin/env bash
# Cross-compile the release archives.
#
# Everything here is pure Go (CGO_ENABLED=0): webframe loads the system WebView
# at runtime through purego, and golem loads libunicorn the same way, so no C
# toolchain and no per-platform runner is needed to build the matrix.
#
# Not shipped: linux-musl. purego without cgo needs glibc, so Alpine would need
# a cgo build (zig cc) per architecture — a separate toolchain for a niche
# target. See docs/ROADMAP.md.
set -euo pipefail

version="${1:-}"
if [ -z "$version" ]; then
  echo "usage: $0 <version>   e.g. $0 v0.1.0" >&2
  exit 2
fi
out="${OUT:-dist}"
rm -rf "$out"
mkdir -p "$out"

targets=(linux/amd64 linux/arm64 darwin/amd64 darwin/arm64 windows/amd64 windows/arm64)

for target in "${targets[@]}"; do
  os="${target%/*}"
  arch="${target#*/}"
  ext=""
  [ "$os" = windows ] && ext=".exe"
  bin="runvm${ext}"

  printf '→ %-14s ' "${os}/${arch}"
  CGO_ENABLED=0 GOOS="$os" GOARCH="$arch" go build -tags unicorn -trimpath \
    -ldflags "-s -w -X main.version=${version}" -o "$out/$bin" ./cmd/runvm

  # zip per target: Windows users expect it, and it keeps one archive format.
  python3 - "$out" "$bin" "runvm_${version#v}_${os}_${arch}.zip" <<'PY'
import os, sys, zipfile
out, binname, zname = sys.argv[1:4]
with zipfile.ZipFile(os.path.join(out, zname), "w", zipfile.ZIP_DEFLATED) as z:
    z.write(os.path.join(out, binname), arcname=binname)
PY
  rm -f "$out/$bin"
  echo "$(du -h "$out/runvm_${version#v}_${os}_${arch}.zip" | cut -f1)"
done

(
  cd "$out"
  sha256sum ./*.zip > SHA256SUMS
)

echo
ls -lh "$out"
