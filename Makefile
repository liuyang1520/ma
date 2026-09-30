export GOTOOLCHAIN := local
export GOWORK := off
export GOMODCACHE := $(CURDIR)/.cache/mod
export GOCACHE := $(CURDIR)/.cache/go
export GOPROXY := off
export GOFLAGS := -mod=readonly

PREFIX ?= $(HOME)/.local

.PHONY: deps build test integration bench bench-compression release install clean
deps:
	python3 scripts/deps.py download
build:
	go build -trimpath -o bin/ma ./cmd/ma
test:
	go test ./...
	python3 -m unittest discover -s scripts -p '*_test.py'
integration: build
	python3 scripts/pty_test.py
bench:
	go test ./internal/pager -run '^$$' -bench BenchmarkScrollTransport -benchmem
	go test ./internal/render -run '^$$' -bench 'BenchmarkNative(Canvas|Layout)$$' -benchmem
bench-compression:
	go test ./internal/pager -run '^$$' -bench BenchmarkBandCompression -benchmem
release:
	python3 scripts/release.py
install: build
	install -d "$(DESTDIR)$(PREFIX)/bin"
	install -m 755 bin/ma "$(DESTDIR)$(PREFIX)/bin/ma"
	install -d "$(DESTDIR)$(PREFIX)/share/doc/ma"
	install -m 644 LICENSE "$(DESTDIR)$(PREFIX)/share/doc/ma/LICENSE"
	install -m 644 THIRD_PARTY_NOTICES.txt "$(DESTDIR)$(PREFIX)/share/doc/ma/THIRD_PARTY_NOTICES.txt"
clean:
	rm -rf bin artifacts
