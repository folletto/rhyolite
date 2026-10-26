#!/bin/sh
# Build the macOS app, which wraps the CLI. Needs macOS with the Swift toolchain.
#   scripts/build-app.sh        -> bin/Rhyolite.app
#   scripts/build-app.sh --dmg  -> dist/Rhyolite-macos.dmg
# The app's version and build number are the ones in macos/Info.plist, copied
# as-is. The embedded CLI is built by scripts/build.sh with that same version,
# unless VERSION is set. scripts/build.sh never depends on this script.
set -eu
cd "$(dirname "$0")/.."

if [ "$(uname -s)" != Darwin ] || ! command -v swift >/dev/null 2>&1; then
	echo "building the app needs macOS with the Swift toolchain (Xcode or Command Line Tools)" >&2
	exit 1
fi

plist_get() { /usr/libexec/PlistBuddy -c "Print :$1" macos/Info.plist; }

VERSION="${VERSION:-$(plist_get CFBundleShortVersionString)}"
export VERSION

# build_app <output dir>: builds Rhyolite.app in macos/.build, then, as the very
# last step, moves it to <output dir>. A failed build never touches the output dir.
build_app() {
	out="$1"
	stage="macos/.build/stage"
	app="$stage/Rhyolite.app"
	rm -rf "$stage"
	mkdir -p "$app/Contents/MacOS" "$app/Contents/Helpers" "$app/Contents/Resources"

	# Universal CLI, embedded as-is.
	scripts/build.sh --target darwin arm64 "$stage/rhyolite-arm64"
	scripts/build.sh --target darwin amd64 "$stage/rhyolite-amd64"
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
	echo "built $out/Rhyolite.app ($(plist_get CFBundleShortVersionString), build $(plist_get CFBundleVersion))"
}

case "${1:-}" in
"")
	build_app bin
	;;
--dmg)
	build_app dist
	hdiutil create -quiet -volname Rhyolite -srcfolder dist/Rhyolite.app -ov -format UDZO dist/Rhyolite-macos.dmg
	rm -rf dist/Rhyolite.app
	echo "built dist/Rhyolite-macos.dmg"
	;;
*)
	echo "usage: $0 [--dmg]" >&2
	exit 2
	;;
esac
