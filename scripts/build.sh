#!/bin/sh
# Build the rhyolite binary.
#   scripts/build.sh          -> bin/rhyolite for this machine
#   scripts/build.sh --all    -> dist/rhyolite-<os>-<arch>[.exe] for all supported platforms
set -eu
cd "$(dirname "$0")/.."

VERSION="${VERSION:-$(git describe --tags --always --dirty 2>/dev/null || echo dev)}"
LDFLAGS="-s -w -X main.version=${VERSION}"
export CGO_ENABLED=0

build() { # os arch out
	echo "building $3 ($1/$2)"
	GOOS="$1" GOARCH="$2" go build -trimpath -ldflags "$LDFLAGS" -o "$3" ./cmd/rhyolite
}

case "${1:-}" in
"")
	mkdir -p bin
	go build -trimpath -ldflags "$LDFLAGS" -o bin/rhyolite ./cmd/rhyolite
	echo "built bin/rhyolite ($VERSION)"
	;;
--all)
	rm -rf dist
	mkdir -p dist
	build darwin arm64 dist/rhyolite-darwin-arm64
	build darwin amd64 dist/rhyolite-darwin-amd64
	build linux amd64 dist/rhyolite-linux-amd64
	build linux arm64 dist/rhyolite-linux-arm64
	build windows amd64 dist/rhyolite-windows-amd64.exe
	build windows arm64 dist/rhyolite-windows-arm64.exe
	;;
*)
	echo "usage: $0 [--all]" >&2
	exit 2
	;;
esac
