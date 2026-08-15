SHELL := /usr/bin/env bash
.DEFAULT_GOAL := help

IMAGE_REF ?= dar-download-app:local

.PHONY: help bootstrap format test test-security prebuild image postbuild candidate

help:
	@printf '%s\n' \
	  'make bootstrap       Install the pinned Go toolchain and Go security tools' \
	  'make format          Format Go and shell source' \
	  'make test            Run the fast application suite' \
	  'make test-security   Run coverage, race, and bounded fuzz checks' \
	  'make prebuild        Run the hard source-security gate' \
	  'make image           Build only from current passing pre-build evidence' \
	  'make postbuild       Scan and DAST the exact built image' \
	  'make candidate       Run prebuild, image, and postbuild in order'

bootstrap:
	mise install
	./scripts/bootstrap-tools.sh
	./scripts/check-tools.sh

format:
	gofmt -w cmd internal
	shfmt -w -i 2 -ci scripts

test:
	go test -count=1 ./...

test-security:
	go test -count=1 -cover ./internal/auth ./internal/blob ./internal/config \
	  ./internal/download ./internal/httpapi ./internal/strictjson
	go test -count=1 -race ./...
	go test -run='^$$' -fuzz=FuzzParseEnvironmentPolicy -fuzztime=100000x ./internal/config
	go test -run='^$$' -fuzz=FuzzAuthenticatePrincipalHeader -fuzztime=100000x ./internal/auth
	go test -run='^$$' -fuzz=FuzzSelectRange -fuzztime=100000x ./internal/download

prebuild:
	./scripts/prebuild.sh

image:
	IMAGE_REF='$(IMAGE_REF)' ./scripts/build-image.sh

postbuild:
	IMAGE_REF='$(IMAGE_REF)' ./scripts/postbuild.sh

candidate: prebuild image postbuild
