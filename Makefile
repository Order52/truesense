# TrueSense developer shortcuts
.PHONY: test check lint install uninstall doctor module-test ci release-check
test: go-test    ## unit tests against the firmware simulator (no hardware needed)
lint: go-lint    ## vet, staticcheck, golangci-lint, gofmt
check: go-ci     ## everything CI's Go jobs run
ci:              ## what CI's static job runs, locally (tools that are not installed are skipped): the gate before you push
	@$(GO) run ./cmd/release check
	@if command -v actionlint >/dev/null 2>&1; then actionlint && echo "ok  actionlint"; else echo "skip actionlint"; fi
	@tools/ci/verify-units.sh | tail -3
	@$(GO) run ./cmd/checklinks
	@tools/ci/check-shell.sh
	@$(MAKE) --no-print-directory go-ci
release-check:   ## do the version numbers, notes and tag agree?  (make release-check TAG=v0.4.0)
	$(GO) run ./cmd/release check $(if $(TAG),--tag $(TAG),)
install: build   ## one command: kernel module (DKMS), daemon, user services, completions (needs sudo)
	sudo tools/install.sh
uninstall:
	sudo tools/uninstall.sh
doctor: build
	./dist/truesense doctor
module-test:     ## build the kernel module for every installed kernel (warnings on)
	@tools/ci/build-modules.sh

# ---- the Go program ---------------------------------------------------------------------------------------------------------------------------
.PHONY: build go-test go-race go-cover go-lint go-vuln go-fuzz go-ci go-js
GO       ?= go
COMMIT   := $(shell git rev-parse --short=12 HEAD 2>/dev/null)
DATE     := $(shell git log -1 --format=%cd --date=format-local:%Y-%m-%dT%H:%M:%SZ 2>/dev/null)
PKG      := github.com/Order52/truesense/internal/buildinfo
LDFLAGS  := -s -w -X $(PKG).Commit=$(COMMIT) -X $(PKG).Date=$(DATE)
GOFILES  := $(shell find cmd internal -name '*.go' -not -name '*_test.go') $(shell find internal -path '*/web/*' -type f) go.mod go.sum
build: dist/truesense   ## the static, reproducible binary (dist/truesense; truesense-fast is a link to it)
dist/truesense: $(GOFILES)
	@mkdir -p dist
	CGO_ENABLED=0 $(GO) build -trimpath -buildvcs=false -ldflags "$(LDFLAGS)" -o $@ ./cmd/truesense
	@ln -sf truesense dist/truesense-fast
go-test:         ## unit tests (the firmware simulator stands in for the hardware)
	$(GO) test -count=1 -shuffle=on ./...
go-race:         ## the same with the race detector (what CI runs)
	$(GO) test -race -count=1 -shuffle=on ./...
go-cover:        ## coverage report
	$(GO) test -count=1 -coverprofile=coverage.out ./... && $(GO) tool cover -func=coverage.out | tail -1
go-lint:         ## vet, staticcheck and golangci-lint (the ones that are installed) and gofmt
	$(GO) vet ./...
	@if command -v staticcheck >/dev/null 2>&1; then staticcheck ./... && echo "ok  staticcheck"; else echo "skip staticcheck"; fi
	@if command -v golangci-lint >/dev/null 2>&1; then golangci-lint run ./... && echo "ok  golangci-lint"; else echo "skip golangci-lint"; fi
	@test -z "$$(gofmt -l cmd internal)" || { gofmt -l cmd internal; echo "gofmt: the files above need formatting"; exit 1; }
go-vuln:         ## known vulnerabilities in the code we actually call
	@if command -v govulncheck >/dev/null 2>&1; then govulncheck ./...; else echo "skip govulncheck (go install golang.org/x/vuln/cmd/govulncheck@latest)"; fi
go-fuzz:         ## every fuzz target for a few seconds (FUZZTIME=30s for longer)
	@for p in $$($(GO) list ./...); do for f in $$($(GO) test -list 'Fuzz.*' $$p 2>/dev/null | grep '^Fuzz'); do echo "fuzz $$p $$f"; $(GO) test -run '^$$' -fuzz "^$$f$$" -fuzztime $${FUZZTIME:-5s} $$p || exit 1; done; done
go-js:           ## the window's front-end logic (needs Node; there is no npm step)
	@if command -v node >/dev/null 2>&1; then node --test internal/gui/jstest/; else echo "skip the front-end tests (node not installed)"; fi
go-ci: go-lint go-race go-vuln go-js   ## what CI's Go jobs run
