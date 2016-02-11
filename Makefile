NO_COLOR=\x1b[0m
OK_COLOR=\x1b[32;01m
ERROR_COLOR=\x1b[31;01m
WARN_COLOR=\x1b[33;01m

export ROOTDIR=$(CURDIR)
export GO15VENDOREXPERIMENT=1

all: test

deps:
	@go get -d -v $(go list ./... | grep -v /vendor/)

savedeps:
	@godep save $(go list ./... | grep -v /vendor/)

format:
	@go fmt $(go list ./... | grep -v /vendor/)

test:
	@GIN_MODE=test ginkgo -r

.PHONY: all deps savedeps format test
