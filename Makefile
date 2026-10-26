.PHONY: build build-cli build-app release test cover fuzz clean

# The app is built only where it can be: on macOS with the Swift toolchain.
CAN_BUILD_APP = [ -x scripts/build-app.sh ] && [ "$$(uname -s)" = Darwin ] && command -v swift >/dev/null 2>&1
SKIP_APP = echo "skipping the macOS app (needs macOS with the Swift toolchain)"

build: build-cli
	@if $(CAN_BUILD_APP); then scripts/build-app.sh; else $(SKIP_APP); fi

build-cli:
	@scripts/build-cli.sh

build-app:
	@scripts/build-app.sh

release:
	@scripts/build-cli.sh --all
	@if $(CAN_BUILD_APP); then scripts/build-app.sh --dmg; else $(SKIP_APP); fi

test:
	@scripts/test.sh

cover:
	@scripts/test.sh --cover

fuzz:
	@scripts/test.sh --fuzz

clean:
	rm -rf bin dist coverage.out macos/.build
