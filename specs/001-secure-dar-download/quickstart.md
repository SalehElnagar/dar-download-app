# Validation Quickstart

This guide validates local source and image evidence only. It does not read or change Azure,
Entra, GitHub, customer data, or a live DAR release.

## Prerequisites

- Docker Desktop with Linux containers
- `mise`
- Network access to retrieve pinned Go modules, scanner databases, and container images
- At least 8 GiB free disk space for security databases and intermediate images

Install the repository-pinned Go toolchain and checksum-verified security tools:

```bash
make bootstrap
```

Expected Go version: `go1.26.6`.

## Test-first evidence

The configuration, identity, range, Blob adapter, HTTP handler, integration, and server-bound
tests were observed failing for their missing behavior before each implementation was added.
The final hardening test also failed with a missing `Cross-Origin-Resource-Policy` header before
the handler added the exact `same-origin` value. Only green results are retained as release
evidence; red development output never authorizes a build.

## Application tests

Run fast correctness and contract tests:

```bash
make test
```

Run the slower language-level assurance suite:

```bash
make test-security
```

Expected outcomes:

- All tests pass.
- Statement coverage is at least 90%.
- The race detector reports no race.
- Every bounded fuzz target completes without a failure.
- The 30 MiB synthetic download and range resume are byte-correct.
- The conservative in-process integration heap remains below 96 MiB while the 30 MiB object is
  streamed and hashed without client-side whole-file buffering.
- Denied requests perform no storage operations.

## Pre-build hard gate

```bash
make prebuild
```

This must finish successfully before `make image` accepts a build. It includes format, vet,
static analysis, tests, race, fuzz smoke, module verification, dependency vulnerability
checks, SAST, filesystem/dependency scanning, and a final secret scan. A secret finding stops
the workflow immediately and is never bypassed.

## Build the exact local candidate

```bash
make image IMAGE_REF=dar-download-app:local
```

The build script checks fresh pre-build evidence for the current source tree. It creates the
image only when that evidence matches. The initial release target is exactly `linux/amd64`,
regardless of the developer workstation architecture.

## Post-build hard gate

```bash
make postbuild IMAGE_REF=dar-download-app:local
```

Expected outcomes:

- The scripts record the immutable local image digest.
- SPDX and CycloneDX SBOMs are generated under ignored local evidence storage.
- Trivy and Grype report no Critical or High finding.
- Image metadata and the executable identify Linux AMD64, the non-root runtime user, and no
  shell/package manager.
- The container passes health and denial smoke tests with a read-only root filesystem, all
  capabilities dropped, and `no-new-privileges` enabled.
- OWASP ZAP scans the OpenAPI surface against the same synthetic container on an internal
  no-Internet network, performs no runtime add-on update, and reports no warning or failure.
- Positive and negative controls prove the publish boundary rejects mismatched source, image,
  post-build, SBOM, or provenance identity.

## Complete local candidate

```bash
make candidate IMAGE_REF=dar-download-app:local
```

This runs pre-build, image build, and post-build in that order. It does not publish, sign,
deploy, or change an external system.

## Evidence interpretation

A successful run means only that the exact local candidate passed the named tools with their
then-current databases. It does not prove absolute security or live platform correctness.
Production promotion still requires review of GitHub evidence, signature/provenance
verification, a separately authorized live Entra/private-network/download test, and a staging
penetration test.

## Failure handling

- Do not build or release when any required gate fails or does not run.
- Do not add a broad ignore to make a result green.
- The initial repository supports no automated security exception. A verified false positive or
  accepted risk keeps the candidate blocked until a separately reviewed policy change exists;
  secret findings can never be excepted.
- Rebuild a new immutable digest after remediation. Never overwrite or reinterpret an existing
  release tag.
