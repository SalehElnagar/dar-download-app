# Requirement-to-Evidence Map

This self-contained map identifies what the repository can prove locally and what remains a
protected GitHub or live-platform responsibility. Passing evidence applies only to the exact
recorded source digest and image ID.

## Functional requirements

| Requirement | Primary implementation and evidence |
| --- | --- |
| Only anonymous health and protected release download routes exist | Canonical OpenAPI contract and repository route/status contract tests |
| Trusted OIDC identity requires one exact configured issuer and one bounded opaque subject | Configuration, identity, fuzz, and handler tests covering malformed, duplicate, mismatched, and trailing-slash cases |
| Authentication alone never grants a release | Exact case-sensitive `allowed_subjects` policy and denial-before-storage tests |
| Customer input never selects storage authority or a Blob path | Opaque route validation, server-owned mapping, request-token SAST rule, and Blob adapter tests |
| Blob delivery uses dedicated storage identity and one object version | Managed-identity Blob adapter plus ETag-bound full and single-range tests |
| Transfers and failures are bounded | Header/config/object/time limits, sequential 4 MiB reads, cancellation/error tests, and redacted log tests |
| Product repository excludes deployment, customer, release, and local AI-operation artifacts | Tracked-tree, ignored-path, extension, link, and workflow-policy tests |
| Source gates precede image construction | Unit, integration, contract, race, deterministic fuzz, dependency, SAST, secret, and workflow-order checks |
| Final image is minimal and independently checked | Digest-pinned static build; Linux AMD64 manifest and ELF checks; distroless non-root policy; two image scanners; offline fail-closed ZAP scan |
| Security exceptions cannot silently weaken the candidate | Exactly empty exception registry enforced by repository test and `SECURITY.md` |
| Publishing is immutable, least-privilege, signed, and provenance-bound | Pinned release workflow with cross-job candidate/provenance readback and reviewed dependency proposals |

## Measurable outcomes

| Outcome | Evidence or boundary |
| --- | --- |
| 30 MiB streaming stays byte-correct and bounded | Integration test asserts exact bytes, sub-96 MiB conservative heap, maximum 4 MiB storage opens, and one active reader |
| Full and single-range behavior stays exact | Table and fuzz tests cover full, open, suffix, validator, malformed, multiple, and unsatisfiable ranges |
| Unproven requests never touch storage | Denial matrix asserts zero storage calls for invalid identity, disallowed subject, and invalid/unknown release |
| Core correctness stays exercised | Pre-build enforces at least 90% core statement coverage, race, and bounded fuzz completion |
| No image is built from unvalidated source | Source digest and matching passing pre-build marker are mandatory before image construction |
| Packaged runtime remains constrained | Post-build runs the exact image read-only, non-root, capability-free, and checks for no shell or package manager |
| Required scanners remain release-blocking | Gitleaks, Govulncheck, Gosec, Semgrep, Trivy filesystem/image, Grype, and ZAP all fail closed |
| Evidence stays candidate-bound | SPDX and CycloneDX SBOMs plus transfer-integrity controls bind source, image, platform, revision, and provenance |
| Gate errors never count as clean results | Scanner errors, warnings where applicable, stale evidence, architecture mismatch, and skipped or failed gates return non-zero |

## Evidence that cannot be produced locally

Before production promotion, the owner must obtain an independent code/security review, a
successful run in the private GitHub repository, signature and attestation verification for the
registry digest, and an authorized staging test covering the chosen external OIDC flow, token or
session validation, caller-header stripping, private ingress, exact entitlement, private Blob
access, full/resumed checksum, and penetration testing. None of those live results may be
inferred from local evidence.
