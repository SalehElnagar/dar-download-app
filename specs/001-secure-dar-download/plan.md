# Implementation Plan: Secure DAR Download Application

**Branch**: `main` | **Date**: 2026-08-15 | **Spec**: [spec.md](spec.md)

**Input**: Feature specification from `specs/001-secure-dar-download/spec.md`

## Summary

Replace the verified Python proof-of-concept implementation with one small Go service while
preserving its public HTTP, identity, authorization, Blob-version, and byte-range behavior.
Use the standard HTTP library and only the Azure identity and Blob SDK modules in production.
Package the statically linked binary in a pinned distroless non-root image. A deterministic
local workflow runs tests and source security checks before building, then generates an SBOM,
uses two image vulnerability engines, verifies runtime policy, and runs local OWASP ZAP DAST
against the exact image before it can be considered a release candidate.

## Technical Context

**Language/Version**: Go 1.26.6

**Primary Dependencies**: Go standard library; Azure Identity for Go 1.14.0; Azure Blob Storage
for Go 1.8.0

**Storage**: Existing private Azure Blob Storage reached with one user-assigned managed
identity; no local persistence

**Testing**: Standard Go tests, HTTP recorder integration tests, race detector, statement
coverage, native fuzz targets, synthetic Blob doubles, container smoke tests, and OWASP ZAP
local API scan

**Target Platform**: Linux `amd64` container on Azure Container Apps for the initial release;
the source remains portable, but another architecture requires its own exact-image gates

**Project Type**: Single stateless web service

**Performance Goals**: Byte-correct 30 MiB download; no storage request larger than 4 MiB;
one storage request at a time; peak process memory below 96 MiB in the representative local
test

**Constraints**: Exact tenant/principal/release authorization; maximum 16 KiB identity header,
64 identity claims, 64 KiB release policy, 32 releases, 256 MiB object, one byte range, 4 MiB
storage segment, 10-minute response deadline, 32 KiB aggregate request headers, no SAS or
storage keys, no secrets in logs or images, zero known Critical/High release findings

**Scale/Scope**: One process per replica, one anonymous route, one protected route pattern,
up to 32 configured releases, up to 256 MiB per release, customer concurrency controlled by
the hosting replica limit and HTTP server

## Constitution Check

*GATE: Passed before research and re-checked after design.*

- **Single-Purpose App Boundary**: PASS. The plan contains application code, contracts, tests,
  container packaging, security automation, and documentation only. No Azure or Entra IaC is
  present.
- **Deny by Default and Minimize Authority**: PASS. Trusted platform identity is validated
  again in application code, entitlement is exact per release, and storage uses one managed
  identity without accepting caller credentials or paths.
- **Test-First Contract Preservation**: PASS. Production behavior begins with failing parity
  tests and includes race, fuzz, contract, integration, and denial-side-effect evidence.
- **Reproducible Supply Chain**: PASS. Toolchain, modules, build images, runtime image, and CI
  actions are pinned; image digest, SBOM, provenance, and signature are release subjects.
- **Evidence Over Security Absolutes**: PASS. Gates block Critical/High known findings and tool
  errors while documentation retains the manual staging penetration-test boundary.

## Project Structure

### Documentation (this feature)

```text
specs/001-secure-dar-download/
├── plan.md
├── research.md
├── data-model.md
├── quickstart.md
├── contracts/
│   ├── configuration.md
│   └── http-api.yaml
├── checklists/
│   ├── requirements.md
│   └── security.md
└── tasks.md
```

### Source Code (repository root)

```text
cmd/dar-download/
└── main.go
internal/
├── auth/
│   ├── easyauth.go
│   └── easyauth_test.go
├── blob/
│   ├── azure.go
│   └── azure_test.go
├── config/
│   ├── config.go
│   └── config_test.go
├── download/
│   ├── range.go
│   └── range_test.go
└── httpapi/
    ├── handler.go
    ├── handler_test.go
    └── integration_test.go
api/
└── openapi.yaml
security/
├── exceptions.yaml
├── semgrep.yaml
└── zap-rules.tsv
scripts/
├── prebuild.sh
├── build-image.sh
├── postbuild.sh
└── dast.sh
.github/
├── workflows/
│   ├── ci.yaml
│   └── release.yaml
└── dependabot.yaml
Dockerfile
Makefile
go.mod
go.sum
README.md
SECURITY.md
```

**Structure Decision**: A single service keeps the trusted identity parser, release policy,
range selection, storage boundary, and HTTP adapter separately testable without introducing a
framework or a general-purpose storage proxy. CI and security scripts are repository-root
concerns and the image contains only the compiled command.

## Design Phases

### Phase 0 - Freeze Decisions

- Record Go versus Python, identity trust, exact release policy, sequential range reads,
  server limits, security gates, release evidence, and excluded platform ownership in
  [research.md](research.md).
- Treat official Go and Azure SDK documentation plus the verified Python POC tests as the
  behavior and dependency evidence.

### Phase 1 - Contracts and Test Seams

- Define release, principal, object snapshot, byte range, and release candidate in
  [data-model.md](data-model.md).
- Freeze the HTTP surface in [contracts/http-api.yaml](contracts/http-api.yaml) and runtime
  policy in [contracts/configuration.md](contracts/configuration.md).
- Define local evidence and expected outcomes in [quickstart.md](quickstart.md).

### Phase 2 - Test-First Application

- Create failing configuration, identity, authorization, range, cancellation, storage-version,
  and handler tests.
- Implement only the two-route service and the production Azure adapter.
- Run focused tests after each unit; then run coverage, race, fuzz, and integration suites.

### Phase 3 - Secure Build and Runtime

- Add pinned, multi-stage, non-root distroless packaging after source behavior is green.
- Enforce explicit HTTP and storage limits, read-only runtime validation, dropped capabilities,
  and no-new-privileges. Keep the synthetic 30 MiB byte and resume test at the HTTP integration
  boundary, where storage behavior is deterministic and requires no Azure authority.

### Phase 4 - Supply-Chain and Dynamic Gates

- Make the pre-build script a hard prerequisite for image creation.
- Generate CycloneDX and SPDX SBOMs and scan the exact image digest with Trivy and Grype.
- Run local OWASP ZAP API scanning against the final image using only synthetic configuration.
- Configure tag-only release automation to attest provenance/SBOM and sign the immutable digest.

### Phase 5 - Freeze and Independent Review

- Reconcile every requirement to a task and evidence file.
- Freeze the candidate manifest, run one read-only code/test/docs/security review, correct any
  accepted finding in a new validated candidate, and rerun the full gates.
- Prepare, but do not silently perform, private GitHub repository publication and protection.

## Post-Design Constitution Re-check

All five principles remain PASS. No complexity exception is needed. The single material
residual boundary is deliberate: local DAST cannot prove the live Easy Auth redirect, private
network, managed identity, or tenant configuration, so a separately authorized live browser
test and staging penetration test remain promotion requirements.

## Complexity Tracking

No constitution violation or additional service is introduced.
