# https://github.com/golang-standards/project-layout
# https://medium.com/golang-learn/go-project-layout-e5213cdcfaa2

## version, taken from Git tag (like v1.0.0) or hash
VER:=$(shell (git describe --always --dirty 2>/dev/null || echo "¯\\\\\_\\(ツ\\)_/¯") | sed -e 's/^v//g' )

## fully-qualified path to this Makefile
MKFILE_PATH := $(realpath $(lastword $(MAKEFILE_LIST)))

## fully-qualified path to the current directory
CURRENT_DIR := $(patsubst %/,%,$(dir $(MKFILE_PATH)))

all: default

.PHONY: clean
clean:
	git clean -f -Xd

$(GOPATH)/bin:
	mkdir -p $@

DEP := $(GOPATH)/bin/dep
$(DEP): $(GOPATH)/bin
	curl -sfSL -o $@ https://github.com/golang/dep/releases/download/v0.3.2/dep-$(shell go env GOOS)-$(shell go env GOARCH)
	@chmod +x $@
	@touch $@

vendor: $(DEP) Gopkg.toml
	$(DEP) ensure

GINKGO := $(GOPATH)/bin/ginkgo
$(GINKGO): vendor
	cd vendor/github.com/onsi/ginkgo/ginkgo && go install .

MOCKERY := $(GOPATH)/bin/mockery
$(MOCKERY): vendor
	cd vendor/github.com/vektra/mockery/cmd/mockery && go install .

.PHONY: tools
tools: $(GINKGO) $(MOCKERY)

.PHONY: mocks
#  $(shell go list -f '{{ range .GoFiles }}{{ $$.Dir }}/{{ . }} {{ end }}' ./internal/pkg/interfaces | sed -e 's@$(CURRENT_DIR)/@@g')
mocks: $(MOCKERY)
	$(MOCKERY) -dir=internal/pkg/interfaces -case=underscore -all -inpkg

.PHONY: test
test: $(GINKGO) mocks
	@$(GINKGO) -r

.PHONY: watch-tests
watch-tests: $(GINKGO) mocks
	@$(GINKGO) watch -r

work/ci-job-builder-service-linux-amd64: test
	GOOS=linux GOARCH=amd64 go build -v -o $@ ./cmd/ci-job-builder-service

work/ci-job-builder-service-darwin-amd64: test
	GOOS=darwin GOARCH=amd64 go build -v -o $@ ./cmd/ci-job-builder-service

default: work/ci-job-builder-service-darwin-amd64
default: work/ci-job-builder-service-linux-amd64
