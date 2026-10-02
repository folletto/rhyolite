#!/bin/sh
# Run the checks and tests.
#   scripts/test.sh               -> gofmt check, go vet, tests with the race detector,
#                                    and the macOS app tests when on macOS
#   scripts/test.sh --cover       -> also writes coverage.out and prints a summary
#   scripts/test.sh --fuzz        -> also fuzzes path validation for 30 seconds
#   scripts/test.sh --real-trash  -> also runs the test that uses the real OS trash
set -eu
cd "$(dirname "$0")/.."

cover=0 fuzz=0
for arg in "$@"; do
	case "$arg" in
	--cover) cover=1 ;;
	--fuzz) fuzz=1 ;;
	--real-trash) export RHYOLITE_TEST_REAL_TRASH=1 ;;
	*) echo "usage: $0 [--cover] [--fuzz] [--real-trash]" >&2; exit 2 ;;
	esac
done

unformatted="$(gofmt -l .)"
if [ -n "$unformatted" ]; then
	echo "gofmt needed on:" >&2
	echo "$unformatted" >&2
	exit 1
fi

go vet ./...
# The server must also compile for the other platforms.
for target in linux/amd64 windows/amd64; do
	GOOS="${target%/*}" GOARCH="${target#*/}" CGO_ENABLED=0 go build -o /dev/null ./cmd/rhyolite
done

if [ "$cover" = 1 ]; then
	go test -race -count=1 -coverprofile=coverage.out ./...
	go tool cover -func=coverage.out | tail -n 1
else
	go test -race -count=1 ./...
fi

# The macOS app's tests need macOS and the Swift toolchain; elsewhere they are skipped.
if [ "$(uname -s)" = Darwin ] && command -v swift >/dev/null 2>&1; then
	swift test --package-path macos
else
	echo "skipping macOS app tests (needs macOS with the Swift toolchain)"
fi

if [ "$fuzz" = 1 ]; then
	go test -run '^$' -fuzz FuzzClean -fuzztime 30s ./internal/vault
fi
