.PHONY: run fg stop build clean setup

BUILD_TIME := $(shell date -u +"%Y-%m-%dT%H:%M:%SZ")
PORT := 4242
LOG := notflix.log

build: clean
	go build -ldflags "-X main.buildTime=$(BUILD_TIME)" -o notflix .
	pnpm run build

# SIGTERM, never KILL: library.Shutdown() cancels in-flight ffmpeg and
# jobs.Aria2Stop() runs on the same hook. An orphaned aria2c squats :6800 and
# wedges every later spawn, and orphaned encoders keep writing files the next
# run then stomps. Waits for the port to actually clear, because the successor
# binds it immediately and would otherwise lose the race.
stop:
	@pid=$$(lsof -ti tcp:$(PORT) -sTCP:LISTEN 2>/dev/null); \
	if [ -z "$$pid" ]; then echo "not running"; else \
	  kill -TERM $$pid 2>/dev/null; \
	  for i in $$(seq 1 60); do \
	    lsof -ti tcp:$(PORT) -sTCP:LISTEN >/dev/null 2>&1 || break; sleep 0.25; \
	  done; \
	  if lsof -ti tcp:$(PORT) -sTCP:LISTEN >/dev/null 2>&1; then \
	    echo "pid $$pid ignored SIGTERM after 15s — still on :$(PORT)"; exit 1; \
	  fi; \
	  echo "stopped $$pid"; \
	fi

# Stop, rebuild, start detached. Stopping BEFORE the build is deliberate: `clean`
# unlinks ./notflix, and unlinking the path a running process was started from
# leaves it alive on the deleted inode — which is exactly how the server kept
# serving a build older than the binary on disk, with /api/build reporting a
# timestamp that no file matched.
run:
	@$(MAKE) --no-print-directory stop
	@$(MAKE) --no-print-directory build
	@nohup ./notflix >> $(LOG) 2>&1 &
	@for i in $$(seq 1 60); do \
	  curl -sf localhost:$(PORT)/api/build >/dev/null 2>&1 && break; sleep 0.25; \
	done; \
	if curl -sf localhost:$(PORT)/api/build >/dev/null 2>&1; then \
	  echo "up on :$(PORT) — $$(curl -s localhost:$(PORT)/api/build)  (tail -f $(LOG))"; \
	else \
	  echo "did not come up; last log lines:"; tail -20 $(LOG); exit 1; \
	fi

# Same thing in the foreground, for when you want the log on your terminal.
fg:
	@$(MAKE) --no-print-directory stop
	@$(MAKE) --no-print-directory build
	./notflix

setup:
	@echo "Install faster-whisper: pip install faster-whisper"

clean:
	rm -f notflix
