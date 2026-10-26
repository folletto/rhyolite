#!/bin/sh
# Build the rhyolite CLI. Needs only Go (and git, for the version); it never
# depends on the macOS app or its files.
#   scripts/build-cli.sh                      -> bin/rhyolite for this machine
#   scripts/build-cli.sh --all                -> dist/rhyolite-<os>-<arch>[.exe] for all supported platforms
#   scripts/build-cli.sh --target OS ARCH OUT -> one binary for OS/ARCH at OUT
# The version is VERSION when set (e.g. VERSION=1.2.3), otherwise the one Go
# stamps from git: the tag on the commit, or a pseudo-version naming it.
set -eu
cd "$(dirname "$0")/.."
if ! command -v go >/dev/null 2>&1; then
	echo "building the CLI needs Go (https://go.dev/dl/)" >&2
	exit 1
fi

LDFLAGS="-s -w"
if [ -n "${VERSION:-}" ]; then
	LDFLAGS="$LDFLAGS -X main.version=${VERSION}"
fi
export CGO_ENABLED=0

build() { # os arch out
	echo "building $3 ($1/$2)"
	GOOS="$1" GOARCH="$2" go build -trimpath -ldflags "$LDFLAGS" -o "$3" ./cmd/rhyolite
}

case "${1:-}" in
"")
	mkdir -p bin
	go build -trimpath -ldflags "$LDFLAGS" -o bin/rhyolite ./cmd/rhyolite
	echo "built bin/rhyolite"
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
--target)
	if [ $# -ne 4 ]; then
		echo "usage: $0 --target OS ARCH OUT" >&2
		exit 2
	fi
	build "$2" "$3" "$4"
	;;
*)
	echo "usage: $0 [--all | --target OS ARCH OUT]" >&2
	exit 2
	;;
esac
