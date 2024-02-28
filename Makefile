MODULE   = $(shell env GO111MODULE=on $(GO) list -m)
DATE    ?= $(shell date +%FT%T%z)
PKGS     = $(or $(PKG),$(shell env GO111MODULE=on $(GO) list ./...))
TESTPKGS = $(shell env GO111MODULE=on $(GO) list -f \
			'{{ if or .TestGoFiles .XTestGoFiles }}{{ .ImportPath }}{{ end }}' \
			$(PKGS))
BIN      = $(CURDIR)/bin

GO      = go
TIMEOUT = 15
V = 0
Q = $(if $(filter 1,$V),,@)
M = $(shell printf "\033[34;1m▶\033[0m")

binext=""
ifeq ($(GOOS),windows)
  binext=".exe"
endif

export GO111MODULE=on

.PHONY: all
all: fmt lint vet build

.PHONY: build
build: $(BIN) ; $(info $(M) building executable…) @ ## Build program binary
	$Q CGO_ENABLED=0 $(GO) build \
		-tags release \
		-o $(BIN)/$(notdir $(basename $(MODULE)))$(binext) main.go
# Tools

$(BIN):
	@mkdir -p $@
$(BIN)/%: | $(BIN) ; $(info $(M) building $(PACKAGE)…)
	$Q env GOBIN=$(BIN) $(GO) install $(PACKAGE) \
		|| ret=$$?; \
	   exit $$ret

GOLINT = $(BIN)/golint
$(BIN)/golint: PACKAGE=golang.org/x/lint/golint@latest

STATICCHECK = $(BIN)/staticcheck
$(BIN)/staticcheck: PACKAGE=honnef.co/go/tools/cmd/staticcheck@latest

ERRCHECK = $(BIN)/errcheck
$(BIN)/errcheck: PACKAGE=github.com/kisielk/errcheck@latest

VULNCHECK = $(BIN)/govulncheck
$(BIN)/govulncheck: PACKAGE=golang.org/x/vuln/cmd/govulncheck@latest

# Tests

TEST_TARGETS := test-default test-bench test-short test-verbose test-race
.PHONY: $(TEST_TARGETS) check test tests
test-bench:   ARGS=-run=__absolutelynothing__ -bench=. ## Run benchmarks
test-short:   ARGS=-short        ## Run only short tests
test-verbose: ARGS=-v            ## Run tests in verbose mode
test-race:    ARGS=-race         ## Run tests with race detector
$(TEST_TARGETS): NAME=$(MAKECMDGOALS:test-%=%)
$(TEST_TARGETS): test
check test tests: fmt lint vet staticcheck errcheck vulncheck; $(info $(M) running $(NAME:%=% )tests…) @ ## Run tests
	$Q $(GO) test -timeout $(TIMEOUT)s $(ARGS) $(TESTPKGS)

.PHONY: lint
lint: | $(GOLINT) ; $(info $(M) running golint…) @ ## Run golint
	$Q $(GOLINT) -set_exit_status $(PKGS)

.PHONY: fmt
fmt: ; $(info $(M) running gofmt…) @ ## Run gofmt on all source files
	$Q $(GO) fmt $(PKGS)

.PHONY: vet
vet: ; $(info $(M) running go vet…) @ ## Run go vet on all source files
	$Q $(GO) vet $(PKGS)

.PHONY: staticcheck
staticcheck: | $(STATICCHECK) ; $(info $(M) running staticcheck…) @
	$Q $(STATICCHECK) $(PKGS)

.PHONY: errcheck
errcheck: | $(ERRCHECK) ; $(info $(M) running errcheck…) @
	$Q $(ERRCHECK) $(PKGS)

.PHONY: vulncheck
vulncheck: | $(VULNCHECK) ; $(info $(M) running vulncheck…) @
	$Q $(VULNCHECK) $(PKGS)

# Misc

.PHONY: clean
clean: ; $(info $(M) cleaning…)	@ ## Cleanup everything
	@rm -rf $(BIN)
	@rm -rf test/tests.*

.PHONY: help
help:
	@grep -hE '^[ a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | \
		awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-17s\033[0m %s\n", $$1, $$2}'
