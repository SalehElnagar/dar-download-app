# Requirement-to-Evidence Map

This map identifies what the repository can prove locally and what remains a protected GitHub
or live-platform responsibility. Passing evidence applies only to the exact recorded source
digest and image ID.

## Functional requirements

| Requirements | Primary implementation and evidence |
| --- | --- |
| FR-001–FR-005 | Two-route OpenAPI contract; strict Easy Auth parsing; exact tenant, principal, opaque release mapping, and denial-before-storage tests |
| FR-006–FR-010 | Dedicated managed-identity Blob adapter; ETag-bound full and single-range tests, including strong `If-Range` behavior |
| FR-011–FR-014 | Header/config/object/time bounds, sequential 4 MiB storage segments, cancellation/error tests, and redacted structured-log tests |
| FR-015 | Repository boundary test rejects deployment files, DARs, archives, environment files, and Azure state/configuration |
| FR-016–FR-017 | Unit, integration, contract, race, deterministic fuzz, dependency, SAST, secret, and workflow checks in the pre-build gate |
| FR-018–FR-020 | Digest-pinned static build; Linux AMD64 manifest and ELF checks; distroless non-root policy; two image scanners; offline fail-closed ZAP scan |
| FR-021 | Exactly empty automated exception registry enforced by a repository test and `SECURITY.md` |
| FR-022–FR-023 | Least-privilege, immutable, digest-signing release workflow with cross-job candidate/provenance readback plus reviewed Dependabot proposals |
| FR-024 | Security, operations, threat-model, and staging-assurance documentation |

## Success criteria

| Criterion | Evidence or boundary |
| --- | --- |
| SC-001 | 30 MiB integration test asserts byte correctness, sub-96 MiB conservative integration heap, maximum 4 MiB storage opens, and one active reader |
| SC-002 | Table and fuzz tests cover full, open, suffix, validator, malformed, multiple, and unsatisfiable ranges |
| SC-003 | Denial matrix asserts zero storage calls for unproven requests |
| SC-004 | Pre-build enforces at least 90% core statement coverage, race, and bounded fuzz completion |
| SC-005 | Source digest and passing pre-build marker are mandatory before image construction |
| SC-006 | Post-build runs the exact image read-only, non-root, capability-free, and checks for no shell or package manager |
| SC-007 | Gitleaks, Govulncheck, Gosec, Semgrep, Trivy filesystem/image, Grype, and ZAP are release-blocking |
| SC-008 | Local post-build creates SPDX and CycloneDX SBOMs and tests transfer-integrity rejection; the protected GitHub release job must revalidate every marker, then create and verify the signature and signed attestations before tagging |
| SC-009 | Scanner errors, warnings where applicable, stale evidence, architecture mismatch, and skipped or failed gates return non-zero |
| SC-010 | The synthetic HTTP test proves the 30 MiB full/resumed bytes; the exact image separately passes constrained runtime smoke and offline API DAST |

## Evidence that cannot be produced locally

Before the first production promotion, the owner must obtain an independent code/security
review, a successful run in the private GitHub repository, signature and attestation
verification for the registry digest, and an authorized staging test covering Entra redirect,
trusted-header stripping, exact entitlement, private Blob access, full/resumed checksum, and
penetration testing. None of those live results may be inferred from local evidence.
