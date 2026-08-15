<!--
Sync Impact Report
- Version change: template -> 1.0.0
- Added principles: Single-Purpose App Boundary; Deny by Default and Minimize Authority;
  Test-First Contract Preservation; Reproducible Supply Chain; Evidence Over Security Absolutes
- Added sections: Security and Release Gates; Development Workflow
- Removed sections: none
- Deferred items: none
-->
# DAR Download App Constitution

## Core Principles

### I. Single-Purpose App Boundary
The repository MUST contain only the DAR download service, its public contract, tests,
container packaging, security policy, build automation, and developer documentation.
Cloud infrastructure, tenant configuration, customer data, DAR artifacts, deployment
state, and unrelated Harmony code MUST remain outside this repository. The runtime MUST
provide only an anonymous liveness route and authenticated, authorized release downloads.

Rationale: A narrow repository and runtime reduce blast radius, review surface, and
accidental ownership of shared platform resources.

### II. Deny by Default and Minimize Authority
Every download MUST require trusted platform identity evidence, an exact configured tenant,
and an exact per-release principal allowlist. Untrusted request data MUST NOT become a Blob
container or object name. The service MUST use a dedicated managed identity for Blob read
access and MUST NOT accept storage keys, SAS URLs, or inbound user tokens as storage
credentials. Missing, malformed, ambiguous, or unexpected configuration and identity data
MUST fail closed before any storage read.

Rationale: Authentication proves identity but does not establish entitlement. Explicit
authorization and non-delegated workload identity prevent confused-deputy access.

### III. Test-First Contract Preservation (NON-NEGOTIABLE)
Behavior changes MUST begin with an executable test that fails for the intended reason.
The service MUST preserve the verified POC contract for health, authentication,
authorization, exact release mapping, full streaming, one byte range, strong-ETag
`If-Range`, and bounded errors. Unit, integration, race, fuzz, and contract tests MUST pass
before an image is built. A regression that weakens a denial path blocks release.

Rationale: Security controls are reliable only when their failure behavior is frozen and
repeatable.

### IV. Reproducible Supply Chain
Language, dependencies, build images, runtime images, and CI actions MUST be version-pinned;
release images MUST be immutable by digest. The build MUST run as an unprivileged user in a
minimal runtime image, produce an SBOM and provenance, and support signature verification.
Source and dependency security checks MUST pass before image construction. Image and DAST
checks MUST pass before publication or promotion.

Rationale: Source correctness alone does not secure the dependencies, builder, image, or
release path that customers ultimately receive.

### V. Evidence Over Security Absolutes
The project MUST NOT claim that the service is "100% secure." It MAY claim only the exact,
dated evidence produced by the defined gates. A releasable candidate MUST have no known
Critical or High findings in the configured source, dependency, secret, and image scanners,
and no unresolved exploitable High-risk DAST result. Tool failures and skipped required gates
MUST fail the release rather than be treated as success.

Rationale: Security is a maintained risk posture, not a permanent property proven by one
scan.

## Security and Release Gates

- Secrets, credentials, access tokens, customer identifiers, production DAR files, and
  sensitive raw headers MUST NOT enter source, tests, logs, images, reports, or artifacts.
- Requests, headers, decoded claims, configuration documents, ranges, filenames, and object
  metadata MUST have explicit size and count limits.
- Successful body reads MUST be bound to the object version observed during metadata lookup.
- Logs MUST be structured and MUST NOT contain identity headers, tokens, storage URLs, object
  paths, release policy JSON, or customer data.
- Pre-build gates MUST include formatting, static analysis, unit/integration tests, race
  detection, bounded fuzzing, dependency vulnerability analysis, SAST, and secret scanning.
- Post-build gates MUST include SBOM generation, independent image vulnerability scanners,
  non-root/minimal-image policy checks, and local DAST against a synthetic test configuration.
- A Critical or High finding, a secret finding, an integrity failure, or a required tool error
  blocks image publication. An exception requires a dated, owner-approved risk record with an
  expiry and compensating controls; CI MUST NOT silently suppress it.
- A human-authorized staging penetration test remains required before production promotion.

## Development Workflow

1. Freeze requirements, threat boundaries, and HTTP/configuration contracts before source
   implementation.
2. Demonstrate red tests, implement the smallest secure behavior, and refactor only while the
   focused suite remains green.
3. Review production code, test quality, documentation, and security evidence independently
   after the candidate is frozen.
4. Build the image only from the reviewed candidate. Generate SBOM, scan, attest, and sign the
   exact resulting digest.
5. Publish only through protected GitHub automation with least-privilege permissions,
   immutable tags, review requirements, and an auditable release record.
6. Keep deployment and tenant changes in a separately authorized platform workflow. This
   repository does not grant authority to mutate Azure, Entra, or live customer systems.

## Governance

This constitution takes precedence over repository conventions and feature plans. Every
change MUST be reviewed against its principles and release gates. Amendments require a
documented rationale, an explicit semantic-version change, and updates to affected plans,
tests, and automation. Compliance is reviewed at specification, pull-request, image-build,
and release-promotion boundaries. Complexity or gate exceptions MUST be recorded and approved
before they are introduced; convenience is not sufficient justification.

**Version**: 1.0.0 | **Ratified**: 2026-08-15 | **Last Amended**: 2026-08-15
