.PHONY: build app release test cover fuzz clean

build:
	@scripts/build.sh

app:
	@scripts/build-app.sh

release:
	@scripts/build.sh --all
	@if [ -x scripts/build-app.sh ] && [ "$$(uname -s)" = Darwin ] && command -v swift >/dev/null 2>&1; then \
		scripts/build-app.sh --dmg; \
	else \
		echo "skipping the macOS app (needs macOS with the Swift toolchain)"; \
	fi

test:
	@scripts/test.sh

cover:
	@scripts/test.sh --cover

fuzz:
	@scripts/test.sh --fuzz

clean:
	rm -rf bin dist coverage.out macos/.build
