VERSION ?= $(shell git describe --tags --always 2>/dev/null || echo dev)
# Application key for OpenSubtitles.com (optional): with it, users only
# enter their account. Never commit it: pass it from the environment.
OPENSUBTITLES_API_KEY ?=
LDFLAGS := -s -w -X cinearr/internal/api.Version=$(VERSION) -X cinearr/internal/subtitles.BuiltinOpenSubtitlesKey=$(OPENSUBTITLES_API_KEY)

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

# Cross-compiles every target into dist/ (GoReleaser does the same in CI).
release: clean
	CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -trimpath -ldflags "$(LDFLAGS) -H=windowsgui" -o dist/cinearr-windows-amd64.exe ./cmd/cinearr
	CGO_ENABLED=0 GOOS=windows GOARCH=arm64 go build -trimpath -ldflags "$(LDFLAGS) -H=windowsgui" -o dist/cinearr-windows-arm64.exe ./cmd/cinearr
	CGO_ENABLED=0 GOOS=darwin  GOARCH=arm64 go build -trimpath -ldflags "$(LDFLAGS)" -o dist/cinearr-macos-arm64 ./cmd/cinearr
	CGO_ENABLED=0 GOOS=darwin  GOARCH=amd64 go build -trimpath -ldflags "$(LDFLAGS)" -o dist/cinearr-macos-amd64 ./cmd/cinearr
	CGO_ENABLED=0 GOOS=linux   GOARCH=amd64 go build -trimpath -ldflags "$(LDFLAGS)" -o dist/cinearr-linux-amd64 ./cmd/cinearr
	CGO_ENABLED=0 GOOS=linux   GOARCH=arm64 go build -trimpath -ldflags "$(LDFLAGS)" -o dist/cinearr-linux-arm64 ./cmd/cinearr

clean:
	rm -rf bin dist e2e-shots
