# Tasks: Secure DAR Download Application

**Input**: Design documents in `specs/001-secure-dar-download/`

**Tests**: Required. Every application behavior follows red-green-refactor. Security and image
gates fail closed and use only synthetic local data.

## Phase 1: Setup

**Purpose**: Freeze repository governance, contracts, toolchain, and app-only boundaries.

- [x] T001 Record constitution, specification, requirements checklist, plan, research, data
  model, HTTP/configuration contracts, security checklist, and quickstart in `.specify/` and
  `specs/001-secure-dar-download/`
- [x] T002 Initialize the pinned Go 1.26.6 module and Azure dependency graph in `mise.toml`,
  `go.mod`, and `go.sum`
- [x] T003 Add repository, Docker, scanner, and local-evidence exclusions in `.gitignore` and
  `.dockerignore`
- [x] T004 Publish the frozen application contract in `api/openapi.yaml`
- [x] T005 Add repository metadata and security ownership in `README.md`, `LICENSE`,
  `CODEOWNERS`, and `SECURITY.md`

---

## Phase 2: Foundational Test Seams

**Purpose**: Create failing contracts and narrow interfaces before production behavior.

**CRITICAL**: User-story implementation does not begin until the focused tests fail for the
expected missing behavior.

- [x] T006 [P] Add failing strict runtime and release policy tests for FR-004, FR-005, FR-013 in
  `internal/config/config_test.go`
- [x] T007 [P] Add failing Easy Auth provider, size, shape, canonical claim, tenant, and ambiguity
  tests for FR-002 in `internal/auth/easyauth_test.go`
- [x] T008 [P] Add failing full/single-range and strong-ETag `If-Range` table tests for FR-008
  through FR-010 in `internal/download/range_test.go`
- [x] T009 [P] Add a deterministic storage double and request identity fixtures in
  `internal/testsupport/storage.go` and `internal/testsupport/identity.go`
- [x] T010 Capture red-test evidence for T006-T008 in ignored local evidence and record the
  expected failures in `specs/001-secure-dar-download/quickstart.md`

**Checkpoint**: Configuration, identity, and range suites fail because their implementations do
not exist.

---

## Phase 3: User Story 1 - Download an Authorized Release (Priority: P1) MVP

**Goal**: Stream the exact full or partial configured DAR for one authorized principal.

**Independent Test**: A synthetic 30 MiB object downloads byte-for-byte, a resumed range returns
only selected bytes, each storage open is at most 4 MiB and sequential, and health performs no
storage operation.

### Tests for User Story 1

- [x] T011 [P] [US1] Add failing ETag-bound sequential segment and Azure error-mapping tests in
  `internal/blob/azure_test.go`
- [x] T012 [P] [US1] Add failing health, full download, range, header, zero-length, and first-read
  failure handler tests in `internal/httpapi/handler_test.go`
- [x] T013 [P] [US1] Add failing 30 MiB HTTP integration and client-cancellation tests in
  `internal/httpapi/integration_test.go`

### Implementation for User Story 1

- [x] T014 [US1] Implement immutable fail-closed runtime and release policy parsing in
  `internal/config/config.go`
- [x] T015 [US1] Implement bounded Easy Auth principal parsing and exact tenant agreement in
  `internal/auth/easyauth.go`
- [x] T016 [US1] Implement single-range normalization and strong `If-Range` selection in
  `internal/download/range.go`
- [x] T017 [US1] Implement object metadata and sequential ETag-bound Azure range opens using the
  exact user-assigned identity in `internal/blob/azure.go`
- [x] T018 [US1] Implement the two-route HTTP handler, exact release authorization, safe headers,
  and streamed segment copying in `internal/httpapi/handler.go`
- [x] T019 [US1] Assemble the bounded HTTP server, managed identity, signals, and graceful
  shutdown in `cmd/dar-download/main.go`
- [x] T020 [US1] Run and preserve focused green evidence for configuration, auth, range, storage,
  handler, and 30 MiB integration suites in ignored `.security/evidence/`

**Checkpoint**: User Story 1 is independently functional with synthetic storage and no Azure
access.

---

## Phase 4: User Story 2 - Deny Every Unproven Download (Priority: P1)

**Goal**: Prove authentication, authorization, parsing, and object-version failures never grant
excess storage authority or disclose sensitive details.

**Independent Test**: Every missing, malformed, ambiguous, wrong-tenant, unlisted-principal,
copied-release, path-like, unknown-release, invalid-range, oversized-object, and first-storage-
failure scenario returns its contracted response; denial cases perform zero storage operations.

### Tests for User Story 2

- [x] T021 [P] [US2] Add failing denial-before-storage, unknown/path-like release,
  oversized-object, storage-failure, and method/route tests in
  `internal/httpapi/handler_test.go`
- [x] T022 [P] [US2] Add config JSON and Easy Auth payload fuzz targets with bounded seed cases in
  `internal/config/config_fuzz_test.go` and `internal/auth/easyauth_fuzz_test.go`
- [x] T023 [P] [US2] Add range and `If-Range` fuzz targets with invariant assertions in
  `internal/download/range_fuzz_test.go`
- [x] T024 [P] [US2] Add stream cancellation, short-body, midstream version/error, and single-
  active-reader tests in `internal/httpapi/integration_test.go`

### Implementation for User Story 2

- [x] T025 [US2] Complete fail-closed error translation, cancellation, response-commit, fixed
  security headers, and structured redacted logging in `internal/httpapi/handler.go` and
  `cmd/dar-download/main.go`
- [x] T026 [US2] Run statement coverage, branch-sensitive denial review, race detection, and all
  bounded fuzz targets and retain ignored local evidence under `.security/evidence/`

**Checkpoint**: User Stories 1 and 2 pass together and every requested denial is proven before
storage.

---

## Phase 5: User Story 3 - Ship a Verifiable Container Image (Priority: P2)

**Goal**: Build only after source gates and release only after the exact image passes independent
post-build gates.

**Independent Test**: `make candidate` stops before build for a deliberately failing pre-build
fixture, builds the pinned non-root image for a clean source candidate, and stops before release
for a deliberately failing post-build policy fixture.

### Tests and Policies for User Story 3

- [x] T027 [P] [US3] Add focused local Semgrep rules and an empty-by-default exact exception
  schema in `security/semgrep.yaml` and `security/exceptions.yaml`
- [x] T028 [P] [US3] Add OWASP ZAP release-blocking policy and the synthetic DAST environment in
  `security/zap-rules.tsv` and `scripts/dast.sh`
- [x] T029 [P] [US3] Add executable source-digest, tool-version, gate-order, and failure-propagation
  self-tests in `scripts/source-digest.sh`, `scripts/check-tools.sh`, and
  `scripts/test-security-workflow.sh`

### Implementation for User Story 3

- [x] T030 [US3] Implement the secret-first source hard gate with format, vet, staticcheck, test,
  coverage, race, fuzz, module integrity, govulncheck, Gosec, Semgrep, Trivy filesystem, and
  final secret re-scan in `scripts/prebuild.sh`
- [x] T031 [US3] Add the digest-pinned multi-stage static build and distroless non-root runtime in
  `Dockerfile`
- [x] T032 [US3] Implement image construction that requires current matching pre-build evidence
  in `scripts/build-image.sh`
- [x] T033 [US3] Implement SPDX/CycloneDX SBOM, Trivy/Grype scanning, metadata/minimal-content,
  read-only/drop-all/no-new-privileges smoke, and DAST orchestration in `scripts/postbuild.sh`
- [x] T034 [US3] Expose ordered bootstrap, test, prebuild, image, postbuild, and candidate targets
  in `Makefile`
- [x] T035 [P] [US3] Add least-privilege pull-request CI with pinned action commits in
  `.github/workflows/ci.yaml`
- [x] T036 [P] [US3] Add tag-only immutable GHCR build, SBOM/provenance attestations, and keyless
  digest signing with pinned action commits in `.github/workflows/release.yaml`
- [x] T037 [P] [US3] Add reviewed dependency update policy for Go modules, Docker bases, and
  GitHub Actions in `.github/dependabot.yaml`
- [x] T038 [US3] Run the full local candidate workflow against the exact image and retain the
  ignored digest-bound evidence under `.security/evidence/`

**Checkpoint**: Source failure prevents image construction; image or DAST failure prevents a
releasable candidate; the successful local candidate has complete digest-bound evidence.

---

## Phase 6: Documentation, Threat Review, and Handoff

**Purpose**: Make residual risk, response, operation, and publication controls explicit.

- [x] T039 [P] Document architecture, local development, configuration, evidence interpretation,
  and the platform responsibility boundary in `README.md` and `docs/operations.md`
- [x] T040 [P] Record trust boundaries, assets, attacker capabilities, abuse paths, mitigations,
  and residual risks in `docs/threat-model.md`
- [x] T041 [P] Record vulnerability reporting, triage SLAs, exception policy, rollback/revocation,
  and disclosure boundaries in `SECURITY.md`
- [x] T042 Run OpenAPI/config contract checks, Markdown link checks, clean-code, test, docs, and
  DevSecOps reviews; correct accepted findings in their owned files
- [ ] T043 Freeze the candidate manifest, reserve one read-only independent reviewer, and rerun
  the complete tests and source/image/DAST gates after any accepted correction. The reserved
  reviewer did not complete; independent review remains required before first publication.
- [x] T044 Reconcile FR-001 through FR-024 and SC-001 through SC-010 to tasks and evidence, mark
  completed tasks in `specs/001-secure-dar-download/tasks.md`, and record the exact non-
  deployment/non-publication boundary
- [x] T045 Prepare the private `SalehElnagar/dar-download-app` repository creation, default-
  branch ruleset, required checks, environment protection, and first-push commands for explicit
  owner execution; do not mix or delete the Python POC history

---

## Dependencies and Execution Order

- T001-T005 establish governance, toolchain, contracts, and repository boundaries.
- T006-T010 freeze independent red tests before production source exists.
- T011-T013 extend the red contract for User Story 1; T014-T019 implement in dependency order.
- T021-T024 extend denial/fuzz/cancellation coverage; T025 completes hardening; T026 is its gate.
- T027-T029 freeze security-workflow contracts; T030-T037 implement build and automation.
- T038 runs only after the complete application and image workflow exists.
- T039-T041 may be written independently after behavior and gates settle.
- T042-T045 are sequential freeze, review, reconciliation, and publication handoff.
- Any Gitleaks finding stops immediately. It is remediated and rescanned before any downstream
  gate; it is never bypassed.

## Parallel Opportunities

- T006-T009 affect independent foundational test files.
- T011-T013 affect storage, handler, and integration tests independently.
- T022-T024 affect independent fuzz/integration files.
- T027-T029 affect independent security policy and workflow-test files.
- T035-T037 affect independent automation files after local commands settle.
- T039-T041 affect independent documentation files after final behavior settles.

One writer owns production and automation changes. One read-only reviewer is reserved only after
the candidate is frozen.

## Implementation Strategy

1. Finish setup and prove the foundational red tests.
2. Implement User Story 1 as the minimum useful service and validate the 30 MiB stream.
3. Complete User Story 2 denial, fuzz, cancellation, and logging hardening.
4. Add User Story 3 source gate, image, post-build, DAST, and release automation in that order.
5. Freeze, independently review, rerun all evidence, and prepare the protected private GitHub
   handoff without deploying or modifying the live tenant.

## Task Format Validation

All 45 tasks use a checkbox, sequential task ID, optional `[P]`, required story label in user-
story phases, a concrete action, and exact repository path(s).
