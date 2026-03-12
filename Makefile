WHISPER_PREFIX  := /opt/homebrew/Cellar/whisper-cpp/1.8.3/libexec
BREW_LIB        := /opt/homebrew/lib

CGO_CFLAGS_WHISPER  := -I$(WHISPER_PREFIX)/include -I/opt/homebrew/include
CGO_LDFLAGS_WHISPER := -L$(BREW_LIB) -L$(WHISPER_PREFIX)/lib

.PHONY: run whisper clean

whisper: clean
	CGO_ENABLED=1 \
	CGO_CFLAGS="$(CGO_CFLAGS_WHISPER)" \
	CGO_LDFLAGS="$(CGO_LDFLAGS_WHISPER)" \
	go build -tags whisper -o notflix .
	pnpm run build

run: whisper
	./notflix

clean:
	rm -f notflix
