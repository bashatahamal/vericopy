GO ?= go
WAILS ?= wails
VERSION := $(shell tr -d '\r\n' < VERSION)
COMMIT ?= $(shell git rev-parse --short=12 HEAD 2>/dev/null || printf unknown)
BUILD_DATE ?= unknown
LDFLAGS := -s -w \
	-X github.com/bashatahamal/vericopy/internal/version.Version=$(VERSION) \
	-X github.com/bashatahamal/vericopy/internal/version.Commit=$(COMMIT) \
	-X github.com/bashatahamal/vericopy/internal/version.BuildDate=$(BUILD_DATE)

.PHONY: all build desktop-build desktop-package test race race-container vet staticcheck vulncheck check integration cross-build clean

all: check build

build:
	mkdir -p bin
	$(GO) build -trimpath -buildvcs=false -ldflags '$(LDFLAGS)' -o bin/vericopy ./cmd/vericopy

desktop-build:
	mkdir -p bin
	$(GO) build -tags desktop -trimpath -buildvcs=false -ldflags '$(LDFLAGS)' -o bin/vericopy-desktop ./cmd/vericopy-desktop

desktop-package:
	cd cmd/vericopy-desktop && $(WAILS) build -clean

test:
	$(GO) test -count=1 ./...

race:
	$(GO) test -race -count=1 ./...

race-container:
	./scripts/race-container.sh

vet:
	$(GO) vet ./...

staticcheck:
	$(GO) run honnef.co/go/tools/cmd/staticcheck@v0.7.0 ./...

vulncheck:
	$(GO) run golang.org/x/vuln/cmd/govulncheck@v1.6.0 ./...

check: test vet staticcheck vulncheck

integration:
	sh ./integration/run.sh

cross-build:
	mkdir -p bin
	GOOS=windows GOARCH=amd64 CGO_ENABLED=0 $(GO) build -trimpath -buildvcs=false -ldflags '$(LDFLAGS)' -o bin/vericopy_windows_amd64.exe ./cmd/vericopy
	GOOS=windows GOARCH=arm64 CGO_ENABLED=0 $(GO) build -trimpath -buildvcs=false -ldflags '$(LDFLAGS)' -o bin/vericopy_windows_arm64.exe ./cmd/vericopy
	GOOS=darwin GOARCH=amd64 CGO_ENABLED=0 $(GO) build -trimpath -buildvcs=false -ldflags '$(LDFLAGS)' -o bin/vericopy_darwin_amd64 ./cmd/vericopy
	GOOS=darwin GOARCH=arm64 CGO_ENABLED=0 $(GO) build -trimpath -buildvcs=false -ldflags '$(LDFLAGS)' -o bin/vericopy_darwin_arm64 ./cmd/vericopy
	GOOS=linux GOARCH=amd64 CGO_ENABLED=0 $(GO) build -trimpath -buildvcs=false -ldflags '$(LDFLAGS)' -o bin/vericopy_linux_amd64 ./cmd/vericopy
	GOOS=linux GOARCH=arm64 CGO_ENABLED=0 $(GO) build -trimpath -buildvcs=false -ldflags '$(LDFLAGS)' -o bin/vericopy_linux_arm64 ./cmd/vericopy

clean:
	rm -rf ./bin ./dist ./coverage
