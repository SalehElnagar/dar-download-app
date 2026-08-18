# Requirement-to-Evidence Map

This self-contained map identifies what the repository can prove locally and what remains a
protected Azure DevOps or live-platform responsibility. Passing evidence applies only to the exact
recorded source digest and image ID.

## Functional requirements

| Requirement | Primary implementation and evidence |
| --- | --- |
| Only anonymous health and protected two-segment download routes exist | Canonical OpenAPI contract plus repository and handler route/status tests |
| Trusted OIDC identity requires one explicit adapter, one exact configured issuer, and one bounded subject | Generic, Entra ACA, and `azure_container_apps_oidc` configuration, identity, fuzz, and handler tests covering malformed, duplicate, cross-mode, tenant, provider, principal-ID, issuer, and trailing-slash cases |
| Every valid authenticated identity may download without an application allowlist | Arbitrary-subject tests in all trusted modes and configuration tests proving no release policy is required |
| The custom ACA OIDC confidential client stays outside Go | Repository controls prohibit client ID/secret inputs; configuration tests require only issuer and bounded provider name; live auth-resource readback remains external evidence |
| The obsolete static authorization setting cannot be silently retained | Startup configuration tests reject `DAR_DOWNLOAD_RELEASES_JSON` whenever supplied |
| Caller input maps only to one safe exact Blob in the fixed container | Two-segment grammar tests, fuzzing, exact ZIP/DAR mapping tests, no-list Blob interface, and Blob adapter tests |
| Authentication precedes Blob existence checks | Handler denial matrix asserts zero storage calls for absent or invalid identity evidence |
| Blob delivery uses dedicated storage identity and one object version | Managed-identity Blob adapter plus ETag-bound full and single-range tests |
| Transfers and failures are bounded | Header/path/object/time limits, sequential 4 MiB reads, cancellation/error tests, and redacted log tests |
| Product repository contains the Go app, worker, publisher, Azure DevOps definitions, and only canonical release ZIPs while excluding recipient data and deployment state | Tracked-tree, ignored-path, extension, link, pipeline, and recipient-file policy tests |
| Release messages are PII-free and publication precedes queue effects | Go publisher tests parse messages through the worker contract and assert all immutable Blob effects exist before send |
| Source gates precede image construction | Unit, integration, contract, race, deterministic fuzz, dependency, SAST, secret, and workflow-order checks |
| Final image is minimal and independently checked | Digest-pinned static build; Linux AMD64 manifest and ELF checks; distroless non-root policy; two image scanners; offline fail-closed ZAP scan |
| Security exceptions cannot silently weaken the candidate | Exactly empty exception registry enforced by repository test and `SECURITY.md` |
| Release publication is immutable, least-privilege, and input-bound | Azure DevOps exact-commit and recipient-digest checks, protected environment, workload federation, Blob version/CAS code, and PII-free message tests |

## Measurable outcomes

| Outcome | Evidence or boundary |
| --- | --- |
| Example ZIP and DAR names select exact two-segment Blob names | Regression tests assert exact Stat/OpenRange names and attachment filenames |
| Unsafe or ambiguous path forms never reach storage | Table tests and native fuzzing cover empty, dot-only, slash, backslash, nested, control, Unicode, malformed, encoded, double-encoded, query, and oversized inputs |
| 30 MiB streaming stays byte-correct and bounded | Integration test asserts exact bytes, sub-96 MiB conservative heap, maximum 4 MiB storage opens, and one active reader |
| Full and single-range behavior stays exact | Table and fuzz tests cover full, open, suffix, validator, malformed, multiple, and unsatisfiable ranges |
| Core correctness stays exercised | Pre-build enforces at least 90% core statement coverage, race, and bounded fuzz completion |
| No image is built from unvalidated source | Source digest and matching passing pre-build marker are mandatory before image construction |
| Packaged runtime remains constrained | Post-build runs the exact image read-only, non-root, capability-free, and checks for no shell or package manager |
| Required scanners remain release-blocking | Gitleaks, Govulncheck, Gosec, Semgrep, Trivy filesystem/image, Grype, and ZAP all fail closed |
| Evidence stays candidate-bound | SPDX and CycloneDX SBOMs plus transfer-integrity controls bind source, image, platform, revision, and provenance |
| Gate errors never count as clean results | Scanner errors, warnings where applicable, stale evidence, architecture mismatch, and skipped or failed gates return non-zero |

## Evidence that cannot be produced locally

Before production promotion, the owner must obtain an independent code/security review, a
successful Azure DevOps run from the private GitHub repository, signature and attestation verification for the
registry digest, and an authorized staging test covering the chosen external OIDC flow, token or
session validation, caller-header stripping, private ingress, private Blob access, full/resumed
checksum, and penetration testing. Live deployment readback must also prove that no platform
per-user restriction contradicts the all-authenticated-users contract. For a custom provider,
readback and a real login must additionally prove the exact provider name, metadata issuer,
audience, callback, confidential Authorization Code flow, Container Apps secret reference, and
absence of credentials from the Go container. None of those live results may be inferred from
local evidence.
