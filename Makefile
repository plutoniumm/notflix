.PHONY: run build clean setup

BUILD_TIME := $(shell date -u +"%Y-%m-%dT%H:%M:%SZ")

build: clean
	go build -ldflags "-X main.buildTime=$(BUILD_TIME)" -o notflix .
	pnpm run build

run: build
	./notflix

setup:
	@echo "Install faster-whisper: pip install faster-whisper"

clean:
	rm -f notflix
