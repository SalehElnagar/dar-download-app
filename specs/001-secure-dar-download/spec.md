# Feature Specification: Secure DAR Download Application

**Feature Branch**: `main`

**Created**: 2026-08-15

**Status**: Approved for local implementation

**Input**: Build a dedicated private application repository for authenticated DAR downloads,
with security gates before and after container image construction.

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Download an Authorized Release (Priority: P1)

As an explicitly authorized customer using the private network, I can open one protected
release link, complete the existing organization sign-in when required, and receive the
exact DAR without a second download click.

**Why this priority**: Delivering a private release to its intended customer is the complete
business purpose of the application.

**Independent Test**: With synthetic identities, a synthetic 30 MiB object, and a local
storage double, request the full object and representative resumable ranges and compare every
returned byte and response field with the contract.

**Acceptance Scenarios**:

1. **Given** a trusted identity from the configured tenant that is authorized for a release,
   **When** the customer opens that release link, **Then** the exact file is downloaded with a
   safe filename and no storage credential or storage URL is exposed.
2. **Given** an authorized customer with an interrupted transfer, **When** the customer
   requests one valid byte range for the current object version, **Then** only those bytes are
   returned with exact range metadata.
3. **Given** an authenticated customer whose session is already established by the hosting
   platform, **When** the customer downloads a 30 MiB release, **Then** the service streams it
   without loading the full object into memory.
4. **Given** the service is running, **When** an anonymous liveness probe is sent, **Then** it
   reports health without accessing identity or storage data.

---

### User Story 2 - Deny Every Unproven Download (Priority: P1)

As the security owner, I can rely on sign-in being separate from release entitlement, so
missing, ambiguous, substituted, copied, or malformed authority never returns file bytes.

**Why this priority**: A successful sign-in alone must never provide access to confidential
customer releases.

**Independent Test**: Exercise missing and malformed identity evidence, a wrong tenant, an
unlisted principal, copied and unknown release identifiers, unsafe path values, invalid
ranges, object replacement, missing objects, and unavailable storage; prove defined errors
and zero unauthorized body reads.

**Acceptance Scenarios**:

1. **Given** identity evidence is absent, malformed, ambiguous, or for another tenant,
   **When** a release is requested, **Then** the request is denied before any storage read.
2. **Given** a signed-in but unlisted principal, **When** a valid release identifier is copied
   from an authorized customer, **Then** the request is denied before any storage read.
3. **Given** an authorized principal, **When** a malformed, path-like, or unknown release
   identifier is substituted, **Then** no caller-controlled storage path is resolved.
4. **Given** an object changes between metadata lookup and body delivery, **When** the service
   starts the read, **Then** it fails instead of mixing bytes from different object versions.
5. **Given** storage is missing or unavailable, **When** a download is attempted, **Then** a
   bounded error is returned without internal details, file bytes, or credentials.

---

### User Story 3 - Ship a Verifiable Container Image (Priority: P2)

As the application owner, I can review a dedicated private repository where an image can be
built only after source gates pass, and can be released only after the exact image passes
independent post-build gates.

**Why this priority**: Customers receive the built artifact, not source code; the source,
dependency, build, and runtime surfaces all need auditable controls.

**Independent Test**: Run the documented local release-candidate workflow against a clean
checkout and prove the pre-build gate precedes image creation, the post-build gate examines
the resulting digest, and any required failure prevents a releasable result.

**Acceptance Scenarios**:

1. **Given** a source, test, dependency, or secret gate fails, **When** the candidate workflow
   runs, **Then** no release image is produced or published.
2. **Given** all source gates pass, **When** the image is built, **Then** it contains only the
   application runtime, runs without root privileges, and produces a machine-readable
   component inventory.
3. **Given** the built image has a Critical or High known vulnerability, violates runtime
   policy, or fails dynamic security testing, **When** post-build gates run, **Then** the image
   is blocked from release.
4. **Given** every required gate passes, **When** a tagged release workflow is authorized,
   **Then** the immutable image is accompanied by verifiable provenance, inventory, scan
   evidence, and a signature.

### Edge Cases

- The identity header is missing, invalid base64, invalid JSON, oversized, contains too many
  claims, contains duplicate conflicting identity claims, or does not describe the configured
  authentication provider.
- A release identifier is empty, too short, too long, percent-encoded, contains a separator,
  or resembles path traversal.
- The release policy is empty, oversized, contains unexpected fields, duplicate object or
  download names, unsafe names, invalid identifiers, or an empty principal allowlist.
- A range is empty, reversed, multiple, out of bounds, overlong, or applied to an empty object.
- `If-Range` is absent, matching, stale, weak, a date, malformed, or oversized.
- The object disappears, changes version, fails before headers, or fails after streaming begins.
- The client disconnects or exceeds the configured request or transfer duration.
- A required scanner is unavailable, exits unexpectedly, or cannot update its vulnerability
  database; this is a gate failure, not a clean result.
- A vulnerability is discovered after release; the response includes triage, revocation, a
  corrected build, and retained evidence rather than rewriting an existing immutable tag.

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: The application MUST expose anonymous liveness at `/healthz` and protected
  downloads at `/v1/releases/<opaque-release-id>/download`; no other application route is in
  scope.
- **FR-002**: A download MUST require trusted hosting-platform identity evidence that resolves
  to exactly one canonical principal in exactly the configured tenant.
- **FR-003**: Every release MUST define a non-empty allowlist of exact principal identifiers;
  tenant authentication alone MUST NOT grant download access.
- **FR-004**: Release identifiers MUST use a narrow opaque format and resolve only through a
  server-owned release mapping.
- **FR-005**: No request path, query, header, token, or requested filename MAY become a storage
  account, container, or object path.
- **FR-006**: The application MUST use its own workload identity for storage and MUST NOT accept
  a storage key, shared access signature, or inbound customer token as a storage credential.
- **FR-007**: Full downloads MUST stream the exact current object, report exact length, advertise
  byte ranges, bind the body read to the observed object version, and use a validated `.dar`
  attachment name.
- **FR-008**: The application MUST support one valid inclusive byte range, including open-ended
  and suffix forms, and return exact partial length and range metadata.
- **FR-009**: Invalid or unsatisfiable ranges MUST return a range error with the complete object
  length and no release bytes; multiple ranges are unsupported.
- **FR-010**: With `Range`, an absent or exact matching strong `If-Range` validator MUST permit
  a partial response. Every stale, weak, date, malformed, or oversized validator MUST ignore
  the range and return the full current representation.
- **FR-011**: File delivery MUST remain bounded in memory, storage-fetch size, concurrency,
  request-header size, configuration size, object size, and transfer duration.
- **FR-012**: Missing objects, unavailable storage, invalid requests, failed authentication, and
  denied authorization MUST have stable bounded responses that expose no internal storage
  details, credentials, raw identity evidence, or file bytes.
- **FR-013**: Runtime configuration MUST fail at startup when a required value is absent,
  malformed, duplicated, ambiguous, oversized, or includes an unexpected field.
- **FR-014**: Logs MUST be structured and MUST NOT contain tokens, identity headers, policy JSON,
  storage URLs, object paths, customer data, or release contents.
- **FR-015**: The repository MUST be dedicated to application source, tests, public contracts,
  container packaging, security policy, build automation, and developer documentation; Azure
  deployment configuration, tenant configuration, state, and DAR files are excluded.
- **FR-016**: The repository MUST define test-first unit, integration, contract, race, and
  bounded fuzz checks for security-sensitive parsing, authorization, range selection, storage
  binding, cancellation, and error behavior.
- **FR-017**: A pre-build gate MUST run formatting, static correctness checks, tests, race
  detection, fuzz smoke tests, dependency integrity and vulnerability checks, SAST, and secret
  scanning before container image construction.
- **FR-018**: The container build MUST use pinned builder and minimal runtime inputs, produce a
  statically linked application where practical, declare a non-root runtime identity, and
  contain no package manager, shell, compiler, source tree, credential, or mutable application
  data.
- **FR-019**: A post-build gate MUST generate an SBOM, use at least two vulnerability engines,
  verify runtime user and image configuration policy, scan the final filesystem, and run local
  dynamic security tests against a synthetic environment.
- **FR-020**: A releasable candidate MUST have zero known Critical or High findings from required
  source, dependency, secret, image, and dynamic scanners. Required tool errors and skipped
  gates MUST block release.
- **FR-021**: Any accepted security exception MUST be explicit, narrowly matched, owned, dated,
  justified with compensating controls, and expire automatically; no broad or silent scanner
  suppression is permitted.
- **FR-022**: A release workflow MUST operate on the exact reviewed image digest, use
  least-privilege automation permissions, generate provenance and SBOM attestations, sign the
  digest, and never overwrite an existing release tag.
- **FR-023**: Dependency and workflow update automation MUST propose reviewed changes without
  automatically weakening security gates or publishing a release.
- **FR-024**: Documentation MUST distinguish local/static evidence from live assurance, state
  that no process guarantees absolute security, define vulnerability response and rollback,
  and require a separately authorized staging penetration test before production promotion.

### Key Entities

- **Release Policy**: An opaque release identifier, one storage object name, one safe download
  name, and the exact principals entitled to download it.
- **Authenticated Principal**: The canonical principal and tenant extracted from trusted
  hosting-platform identity evidence.
- **Object Snapshot**: The authoritative size and strong version validator used to bind body
  delivery to one object representation.
- **Byte Range**: One normalized inclusive interval within the current object.
- **Release Candidate**: One source revision, dependency set, image digest, SBOM, provenance,
  signatures, and complete pre-build and post-build evidence.
- **Security Exception**: A narrow, time-bounded, owner-approved risk decision for one exact
  finding; it is never an implicit scanner bypass.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: A synthetic 30 MiB DAR downloads byte-for-byte correctly while peak application
  memory remains below 96 MiB and each storage payload fetch is no larger than 4 MiB with one
  storage read lane.
- **SC-002**: Full, open-ended, suffix, matching-validator, stale-validator, malformed, multiple,
  and unsatisfiable range scenarios all return their exact contracted status, headers, and bytes.
- **SC-003**: Every missing, malformed, wrong-tenant, unlisted-principal, copied-release,
  malformed-release, and unknown-release denial performs zero storage operations.
- **SC-004**: Required automated tests cover at least 90% of application statements and all
  identified authorization and parsing branches; race and bounded fuzz runs complete without a
  failure.
- **SC-005**: A clean candidate cannot reach image construction unless 100% of required
  pre-build gates complete successfully.
- **SC-006**: The final image runs as non-root, starts with a read-only root filesystem and no
  added Linux capabilities in its local validation, and contains no shell or package manager.
- **SC-007**: Required source, dependency, secret, image, and dynamic scans report zero known
  Critical or High release-blocking findings for the exact candidate digest.
- **SC-008**: Every releasable image digest has one machine-readable SBOM, one provenance
  attestation, one verification-capable signature, and retained gate evidence.
- **SC-009**: A required scanner failure, database failure, timeout, skipped gate, or integrity
  mismatch produces a non-zero workflow result and no published release.
- **SC-010**: A local end-to-end application test uses synthetic policy and storage to download
  the 30 MiB object and a resumed range within two minutes, while the exact packaged image
  independently passes read-only runtime and dynamic API tests.

## Assumptions

- Microsoft Entra and Azure Container Apps Authentication remain the hosting authentication
  boundary; this repository consumes only their trusted identity evidence.
- The hosting platform strips caller-supplied identity headers before invoking the application.
- The runtime receives private network access to Azure Blob Storage and a dedicated managed
  identity with read scope limited to the intended container.
- A separate platform repository owns Azure Container Apps, networking, Entra, DNS, storage,
  role assignments, and deployment state.
- The private GitHub destination is `SalehElnagar/dar-download-app`.
- Synthetic local data is sufficient for repository validation; no production identity,
  customer file, credential, Azure access, publication, or deployment is needed here.
- Browser redirect and callback behavior are platform responsibilities and remain covered by a
  separate authorized live end-to-end test.

## Out of Scope

- Azure, Entra, networking, DNS, storage, or Container Apps provisioning and mutation.
- A custom login page, token exchange, token storage, customer-directory lookup, or entitlement
  database.
- Public Blob access, SAS URLs, storage keys, Blob mounts, Azure Files, inbound-token
  passthrough, multi-range responses, or direct public storage downloads.
- Uploads, listing releases, deleting releases, emailing customers, managing recipients, or
  generating customer links.
- Claiming absolute security or replacing a separately authorized staging penetration test.
