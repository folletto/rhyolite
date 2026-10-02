.PHONY: build app release test cover fuzz clean

build:
	@scripts/build.sh

app:
	@scripts/build.sh --app

release:
	@scripts/build.sh --all

test:
	@scripts/test.sh

cover:
	@scripts/test.sh --cover

fuzz:
	@scripts/test.sh --fuzz

clean:
	rm -rf bin dist coverage.out macos/.build
