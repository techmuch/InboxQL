.PHONY: all build frontend backend install uninstall clean run stop start restart test

PREFIX ?= $(shell go env GOPATH)
BINDIR ?= $(PREFIX)/bin

all: build

build: frontend backend

frontend:
	@echo "Building frontend..."
	npm --prefix frontend install
	npm --prefix frontend run build
	@echo "Copying assets to embedded static directory..."
	rm -rf internal/embed/static/*
	mkdir -p internal/embed/static
	cp -r frontend/dist/* internal/embed/static/

VERSION ?= $(shell node -p "require('./frontend/package.json').version" 2>/dev/null || echo "0.0.7")
LDFLAGS := -X github.com/user/uea/internal/cli.Version=$(VERSION)

backend:
	@echo "Building backend (v$(VERSION))..."
	go build -ldflags "$(LDFLAGS)" -o bin/uea ./cmd/uea

install: build
	@echo "Installing uea binary to $(BINDIR)..."
	mkdir -p $(BINDIR)
	install -m 755 bin/uea $(BINDIR)/uea

uninstall:
	@echo "Removing uea binary from $(BINDIR)..."
	rm -f $(BINDIR)/uea

test:
	@echo "Running backend tests..."
	go test ./...
	@echo "Running frontend tests..."
	npm --prefix frontend test -- --run

clean:
	@echo "Cleaning up build artifacts..."
	rm -rf bin
	rm -rf frontend/dist
	rm -rf web
	rm -f uea.log

# Default to background. Use --foreground for foreground.
# Example: make start --foreground
# `uea init` is safe to re-run and only fills in what is missing, so a fresh
# clone boots in one step while an existing data directory is left alone.
start: stop backend
	@./bin/uea init --data ./data >/dev/null 2>&1 || ./bin/uea init --data ./data --force >/dev/null 2>&1 || true
ifneq (,$(filter --foreground,$(MAKECMDGOALS)))
	@echo "Running backend in foreground..."
	./bin/uea serve --data ./data
else
	@echo "Running backend in background..."
	nohup ./bin/uea serve --data ./data > uea.log 2>&1 &
	@echo "Backend started. Check uea.log for output."
	@echo "Access the frontend at http://localhost:8080"
endif

# Allow --foreground as a flag-like target
--foreground:
	@:

run: start

restart: stop start

stop:
	@echo "Stopping any running backend instances..."
	-pkill -f "bin/uea" || true