VERSION ?= $(shell git describe --tags --always 2>/dev/null || echo dev)
# Application key for OpenSubtitles.com (optional): with it, users only
# enter their account. Never commit it: pass it from the environment.
OPENSUBTITLES_API_KEY ?=
LDFLAGS := -s -w -X cinearr/internal/api.Version=$(VERSION) -X cinearr/internal/update.Version=$(VERSION) -X cinearr/internal/subtitles.BuiltinOpenSubtitlesKey=$(OPENSUBTITLES_API_KEY)

.PHONY: build test lint e2e run release clean

build:
	CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)" -o bin/cinearr ./cmd/cinearr

test:
	go vet ./...
	GOOS=windows go vet ./...
	GOOS=darwin go vet ./...
	go test ./...

# golangci-lint v2 (optional, https://golangci-lint.run), configured in .golangci.yml
lint:
	golangci-lint run ./...

# End-to-end tests against local fakes (Python 3 + Playwright).
e2e:
	tools/e2e/run.sh

run: build
	./bin/cinearr

# Every file of a release in dist/ (archives, Windows installer files,
# Cinearr.app): tools/release/package.sh, as the release workflow does.
# The installer itself is built with Inno Setup on Windows.
release:
	tools/release/package.sh $(patsubst v%,%,$(VERSION))

clean:
	rm -rf bin dist e2e-shots
	rm -f cmd/*/rsrc_windows_*.syso
