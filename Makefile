.PHONY: run build clean setup

build: clean
	go build -o notflix .
	pnpm run build

run: build
	./notflix

setup:
	@echo "Install faster-whisper: pip install faster-whisper"

clean:
	rm -f notflix
