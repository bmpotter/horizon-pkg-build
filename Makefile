ifeq ($(TMPDIR),)
  TMPDIR := /tmp/
endif

ifneq ("$(wildcard ./rules.env)","")
  include rules.env
  export $(shell sed 's/=.*//' rules.env)
endif

EXECUTABLE = $(shell basename $$PWD)

export TMPGOPATH := $(TMPDIR)$(EXECUTABLE)
export PKGPATH := $(TMPGOPATH)/src/github.com/open-horizon/$(EXECUTABLE)
export PATH := $(TMPGOPATH)/bin:$(PATH)

SHELL := /bin/bash
ARCH = $(shell uname -m)
PKGS=$(shell cd $(PKGPATH); GOPATH=$(TMPGOPATH) go list ./... | gawk '$$1 !~ /vendor\// {print $$1}')


COMPILE_ARGS := CGO_ENABLED=0
ifeq ($(ARCH),armv7l)
	COMPILE_ARGS +=  GOARCH=arm GOARM=7
endif

all: $(EXECUTABLE)

ifndef verbose
.SILENT:
endif

$(EXECUTABLE): $(shell find . -name '*.go' -not -path './vendor/*') deps
	@echo "Producing $(EXECUTABLE)"
	cd $(PKGPATH) && \
    export GOPATH=$(TMPGOPATH); \
			$(COMPILE_ARGS) go build -o $(EXECUTABLE)

# let this run on every build to ensure newest deps are pulled
deps: $(TMPGOPATH)/bin/govendor
	@echo "Fetching dependencies"
ifneq ($(GOPATH_CACHE),)
  if [ ! -d $(TMPGOPATH)/.cache ] && [ -e $(GOPATH_CACHE) ]; then \
		ln -s $(GOPATH_CACHE) $(TMPGOPATH)/.cache; \
	fi
endif
	cd $(PKGPATH) && \
		export GOPATH=$(TMPGOPATH); \
      govendor sync

$(TMPGOPATH)/bin/govendor: gopathlinks
	@echo "Fetching govendor"
	mkdir -p $(TMPGOPATH)/bin
		-export GOPATH=$(TMPGOPATH); \
			go get -u github.com/kardianos/govendor

# this is a symlink to facilitate building outside of user's GOPATH
gopathlinks:
ifneq ($(GOPATH),$(TMPGOPATH))
	if [ ! -h $(PKGPATH) ]; then \
		mkdir -p $(shell dirname $(PKGPATH)); \
		ln -s $(CURDIR) $(PKGPATH); \
	fi
endif

clean: mostlyclean
	@echo "Clean"
	find ./vendor -maxdepth 1 -not -path ./vendor -and -not -iname "vendor.json" -print0 | xargs -0 rm -Rf
ifneq ($(TMPGOPATH),$(GOPATH))
	rm -rf $(TMPGOPATH)
endif

mostlyclean:
	@echo "Mostlyclean"
	rm -f $(EXECUTABLE)

lint:
	@echo "Checking source code for style issues and statically-determinable errors"
	-golint ./... | grep -v "vendor/"
	-cd $(PKGPATH) && \
    GOPATH=$(TMPGOPATH) go vet $(shell find . -not -path './vendor/*' -iname '*.go' -print | xargs dirname | sort | uniq | xargs) 2>&1 | grep -vP "^exit.*"

format:
	echo "Formatting all Golang source code with gofmt"
	find . -name '*.go' -not -path './vendor/*' -exec gofmt -l -w {} \;

# only unit tests
test: deps
	@echo "Executing unit tests"
	-cd $(PKGPATH) && \
    GOPATH=$(TMPGOPATH) go test -cover -tags=unit $(PKGS)

test-integration: deps
	@echo "Executing integration tests"
	-cd $(PKGPATH) && \
    GOPATH=$(TMPGOPATH) go test -cover -tags=integration $(PKGS)

check: lint test test-integration

.PHONY: check clean deps format gopathlinks lint mostlyclean pull test test-integration
