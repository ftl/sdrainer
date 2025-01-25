BINARY_NAME = sdrainer
VERSION_NUMBER ?= $(shell git describe --tags | sed -E 's#v##')
GITCOMMIT = $(shell git rev-parse --verify --short HEAD)
BUILDTIME = $(shell date -u +"%Y-%m-%dT%H:%M:%SZ")

ARCH = x86_64
DESTDIR ?=

.PHONY: all
all: clean test build

.PHONY: clean
clean:
	go clean
	rm -f ${BINARY_NAME}

.PHONY: generate
generate:
	go generate ./scope/pb

.PHONY: test
test:
	go test -v -timeout=30s -count 1 ./...

.PHONY: build
build:
	go build -trimpath -buildmode=pie -mod=readonly -modcacherw -v -ldflags "-linkmode internal -extldflags \"${LDFLAGS}\" -X github.com/ftl/sdrainer/cmd.version=${VERSION_NUMBER} -X github.com/ftl/sdrainer/cmd.gitCommit=${GITCOMMIT} -X github.com/ftl/sdrainer/cmd.buildTime=${BUILDTIME}" -o ${BINARY_NAME} .

.PHONY: run
run: build
	go run .
