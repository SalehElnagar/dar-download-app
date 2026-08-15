# Research: Secure DAR Download Application

## Decision 1: Use Go for the production service

**Decision**: Implement the application in Go 1.26.6 using `net/http`, the Azure Identity
module, and the Azure Blob module. Keep the Python POC only as a read-only behavioral oracle
until Go parity is proven.

**Rationale**: This service has a small HTTP surface, long binary streams, strict memory
bounds, and no dynamic application framework needs. Go provides a single static binary,
straightforward streaming and cancellation, native race/fuzz tooling, a smaller runtime image,
and fewer production dependencies than the POC. The team accepts the cost of rewriting and
maintaining Go.

**Alternatives considered**: Keeping Python would minimize rewrite risk and reuse passing
tests, but retains an interpreter, WSGI server, and larger dependency/runtime surface. Rust
could reduce memory further but adds unnecessary implementation and maintenance complexity.

**Primary references**: [Go release information](https://go.dev/dl/),
[Go security policy](https://go.dev/security/),
[Azure Identity for Go](https://pkg.go.dev/github.com/Azure/azure-sdk-for-go/sdk/azidentity),
and [Azure Blob for Go](https://pkg.go.dev/github.com/Azure/azure-sdk-for-go/sdk/storage/azblob).

## Decision 2: Trust only the hosting authentication boundary, then authorize again

**Decision**: Consume Azure Container Apps Authentication identity headers only after the
hosting platform has authenticated the request and stripped caller-supplied versions. Parse
the principal payload with strict size, shape, provider, canonical-identifier, and exact-claim
rules. Require the explicit principal-ID header to agree with exactly one object-ID claim and
one configured tenant claim. Then apply the release allowlist in the application.

**Rationale**: Easy Auth provides the browser/Entra round trip, but platform authentication is
not customer entitlement. The duplicate application check fails closed if configuration or
trusted header evidence is malformed and prevents storage reads for denied users.

**Alternatives considered**: Accepting any tenant user was rejected. Parsing an access token in
the app duplicates platform authentication and key-rotation concerns. Runtime Microsoft Graph
group lookup adds permission, availability, and mutable-policy dependencies. A custom login
service adds another security surface.

## Decision 3: Treat a release identifier as routing, never as authority

**Decision**: Accept only `dar_` plus 16 to 96 ASCII letters, digits, `_`, or `-`. Resolve the
identifier through a complete startup policy that contains the exact Blob name, safe download
name, and non-empty exact principal set. No caller value becomes a Blob name.

**Rationale**: The link may be forwarded or observed. Authorization must remain valid even when
the identifier is known, and resumable GET requests must be idempotent for the same authorized
principal.

**Alternatives considered**: Signed URLs and SAS were rejected because they create browser-
visible bearer capabilities and do not work with a private-endpoint-only storage path. Passing
the URL suffix to Blob was rejected as a path traversal and confused-deputy risk.

## Decision 4: Stream sequential ETag-bound segments

**Decision**: Read object properties first. Normalize the selected full or partial interval
against that size and strong ETag. Fetch at most 4 MiB per Blob request, one request at a time,
with `If-Match` set to the observed ETag. Open the first segment before committing successful
HTTP headers; then copy each segment directly to the response and honor request cancellation.

**Rationale**: Explicit segmentation bounds each SDK/network read independently of defaults,
keeps memory stable for the 30 MiB acceptance artifact, and prevents an object replacement from
silently mixing versions. Opening the first segment before headers preserves a bounded error
when storage fails immediately.

**Alternatives considered**: One unbounded SDK download was rejected because buffering behavior
can drift with SDK defaults. Parallel segments were rejected because they increase memory,
ordering, and retry complexity. Proxying through Azure Front Door or Apigee is a platform
topology decision and does not replace the application storage boundary.

## Decision 5: Support only one RFC-compatible byte range

**Decision**: Support `start-end`, `start-`, and `-suffix` byte ranges. Reject multiple, empty,
reversed, overlong, or unsatisfiable ranges with 416 and `Content-Range: bytes */<size>`. Apply
a range when `If-Range` is absent or exactly matches the current strong ETag. Ignore Range and
return the full current object for stale, weak, date, malformed, or oversized validators.

**Rationale**: One range supports browser retry and resume without multipart response
complexity. Exact strong validators prevent stitching bytes from different representations.

**Alternatives considered**: Multi-range output and date validators add parsing and response
surface without a current customer need. Returning 416 for stale `If-Range` was rejected
because the correct behavior is to ignore Range.

## Decision 6: Fail closed at startup and bound the HTTP server

**Decision**: Require canonical tenant and managed-identity client IDs, narrow storage names,
and a strict JSON release policy with no unknown fields. Cap the policy at 64 KiB and 32
releases. Cap object size at 256 MiB. Configure 5-second header reads, a 15-second request-read
deadline, a 10-minute write deadline, a 60-second idle deadline, and 32 KiB maximum headers.
Graceful shutdown receives 30 seconds.

**Rationale**: A download service must accommodate slow 30 MiB responses without permitting
unbounded headers, configuration, objects, or connections. Startup validation prevents partial
or ambiguous policy from reaching traffic.

**Alternatives considered**: Framework or proxy defaults were rejected because they are not
an explicit application contract. Unlimited objects and write time were rejected as resource-
exhaustion risks.

## Decision 7: Use a two-boundary security gate

**Decision**: `prebuild` runs format, vet, staticcheck, tests, coverage, race, bounded fuzz,
module verification, `govulncheck`, Gosec, Semgrep, Trivy filesystem/dependency scanning, and
Gitleaks. Only a successful prebuild may construct the image. `postbuild` generates SBOMs,
scans the exact image digest with Trivy and Grype, checks image/runtime policy, runs a synthetic
container smoke test, and executes OWASP ZAP API scanning against that same image.

**Rationale**: Source checks cannot observe OS layers or runtime metadata, while image checks
cannot replace language-aware tests and static analysis. Both are necessary and tool errors
must not be interpreted as clean scans.

**Alternatives considered**: One scanner was rejected because vulnerability databases and
package matching differ. A hosted-only scan was rejected because developers need the same
reproducible gate locally. DAST against live customer infrastructure was rejected without
separate authorization.

## Decision 8: Produce verifiable artifacts without claiming absolute security

**Decision**: A tag-only GitHub workflow builds one digest, creates SPDX and CycloneDX SBOMs,
attests provenance and SBOM, and signs the digest using GitHub OIDC/keyless signing. The release
gate is zero known Critical/High findings from required tools, not "100% secure." A human-
authorized staging penetration test is required before production promotion.

**Rationale**: Evidence must remain bound to the exact artifact and build identity. Automated
tools have coverage limits and vulnerability information changes after release.

**Alternatives considered**: Long-lived signing keys were rejected because they add secret
custody. Mutable tags and rebuilding separately for signing were rejected because they break
artifact identity. Treating DAST as a full penetration test was rejected as inaccurate.

## Decision 9: Keep platform and application ownership separate

**Decision**: This repository contains no Terraform, Terragrunt, Bicep, Azure CLI deployment,
Entra registration, or tenant values. It documents the runtime contract but leaves deployment
to the separate platform project and explicit authorization.

**Rationale**: The user requested a GitHub repository for application code and image building.
Mixing live platform ownership would increase blast radius and recreate the boundary the
dedicated repository is intended to establish.

**Alternatives considered**: Copying the POC IaC into this repository was rejected. A combined
application/platform mono-repository could be revisited only through a separate architecture
decision and authority review.
