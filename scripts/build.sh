#!/bin/sh
# Build the rhyolite binary and the macOS app.
#   scripts/build.sh          -> bin/rhyolite for this machine
#   scripts/build.sh --app    -> bin/Rhyolite.app (macOS only; embeds the CLI)
#   scripts/build.sh --all    -> dist/rhyolite-<os>-<arch>[.exe] for all supported platforms,
#                                plus dist/Rhyolite-macos.dmg when run on macOS
# The version comes from macos/Info.plist (CFBundleShortVersionString), the
# single source of truth; the release workflow updates it there.
set -eu
cd "$(dirname "$0")/.."

# plist_string <key>: the <string> value that follows <key> in macos/Info.plist.
plist_string() {
	sed -n "/<key>$1<\/key>/{n;s/.*<string>\(.*\)<\/string>.*/\1/p;}" macos/Info.plist
}

VERSION="$(plist_string CFBundleShortVersionString)"
LDFLAGS="-s -w -X main.version=${VERSION}"
export CGO_ENABLED=0

build() { # os arch out
	echo "building $3 ($1/$2)"
	GOOS="$1" GOARCH="$2" go build -trimpath -ldflags "$LDFLAGS" -o "$3" ./cmd/rhyolite
}

can_build_app() {
	[ "$(uname -s)" = Darwin ] && command -v swift >/dev/null 2>&1
}

# build_app <output dir>: builds Rhyolite.app in macos/.build, then, as the very
# last step, moves it to <output dir>. A failed build never touches the output dir.
build_app() {
	out="$1"
	stage="macos/.build/stage"
	app="$stage/Rhyolite.app"
	rm -rf "$stage"
	mkdir -p "$app/Contents/MacOS" "$app/Contents/Helpers" "$app/Contents/Resources"

	# Universal CLI, embedded as-is.
	build darwin arm64 "$stage/rhyolite-arm64"
	build darwin amd64 "$stage/rhyolite-amd64"
	lipo -create -output "$app/Contents/Helpers/rhyolite" "$stage/rhyolite-arm64" "$stage/rhyolite-amd64"

	# Universal app executable.
	echo "building Rhyolite (swift, arm64+x86_64)"
	swift build -c release --package-path macos --arch arm64 --arch x86_64
	bin_path="$(swift build -c release --package-path macos --arch arm64 --arch x86_64 --show-bin-path)"
	cp "$bin_path/Rhyolite" "$app/Contents/MacOS/Rhyolite"

	# Bundle metadata, copied as-is: version and build number live in macos/Info.plist.
	cp macos/Info.plist "$app/Contents/Info.plist"
	if [ -f macos/Resources/AppIcon.icns ]; then
		# -X drops extended attributes (Finder info, resource fork), which codesign rejects.
		cp -X macos/Resources/AppIcon.icns "$app/Contents/Resources/AppIcon.icns"
	fi

	# Sign inside-out. Ad-hoc by default; SIGN_IDENTITY="Developer ID Application: ..." overrides.
	identity="${SIGN_IDENTITY:--}"
	codesign --force --options runtime --timestamp=none -s "$identity" "$app/Contents/Helpers/rhyolite"
	codesign --force --options runtime --timestamp=none -s "$identity" --entitlements macos/Rhyolite.entitlements "$app"
	codesign --verify --strict "$app"

	# Last step: only a finished, signed app reaches the output folder.
	mkdir -p "$out"
	rm -rf "$out/Rhyolite.app"
	mv "$app" "$out/Rhyolite.app"
	rm -rf "$stage"
	echo "built $out/Rhyolite.app ($VERSION, build $(plist_string CFBundleVersion))"
}

case "${1:-}" in
"")
	mkdir -p bin
	go build -trimpath -ldflags "$LDFLAGS" -o bin/rhyolite ./cmd/rhyolite
	echo "built bin/rhyolite ($VERSION)"
	;;
--app)
	if ! can_build_app; then
		echo "building the app needs macOS with the Swift toolchain (Xcode or Command Line Tools)" >&2
		exit 1
	fi
	build_app bin
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
	if can_build_app; then
		build_app dist
		hdiutil create -quiet -volname Rhyolite -srcfolder dist/Rhyolite.app -ov -format UDZO dist/Rhyolite-macos.dmg
		rm -rf dist/Rhyolite.app
		echo "built dist/Rhyolite-macos.dmg"
	else
		echo "skipping the macOS app (needs macOS with the Swift toolchain)"
	fi
	;;
*)
	echo "usage: $0 [--app|--all]" >&2
	exit 2
	;;
esac
