# Threat Model

## Scope and assets

The protected assets are DAR contents, release entitlements, trusted OIDC identity evidence,
the Blob reader identity, release policy, image integrity, and security evidence. The live OIDC
provider, authentication layer, Azure control plane, and deployment are external dependencies
owned by the platform repository.

## Trust boundaries

1. An untrusted external client reaches the approved HTTPS authentication endpoint.
2. The trusted OIDC layer completes or validates the browser-session or bearer flow.
3. That layer strips caller-supplied identity headers and injects exactly one issuer and subject
   header on private ingress.
4. The Go service exact-matches the configured issuer and the selected release's allowed subject
   before any storage operation.
5. The service obtains a storage token for one dedicated user-assigned identity.
6. Private networking carries ETag-bound reads to the fixed Blob account and container.
7. GitHub Actions converts reviewed source into an immutable signed image and evidence.

The internal identity-header boundary is safe only when the trusted layer strips caller values,
prevents duplicates, and blocks all direct access to application ingress.

## Attacker capabilities and controls

| Abuse path | Primary controls | Residual risk |
| --- | --- | --- |
| Forward a download link | Exact authenticated subject allowlist per opaque release | An authorized recipient can redistribute bytes after download |
| Forge internal identity headers | Header stripping, private ingress, exactly-one checks, bounded values | A compromised or bypassed trusted layer can impersonate users |
| Substitute the issuer or vary a trailing slash | HTTPS issuer validation and byte-exact match to startup policy | A misconfigured upstream can deny valid requests or assert the configured issuer incorrectly |
| Reuse the same subject under another issuer | Issuer and subject are validated as one pair before authorization | Upstream compromise remains authoritative inside its boundary |
| Smuggle duplicate issuer or subject values | Both header value lists must contain exactly one entry | A non-conforming intermediary that rewrites field semantics can cause denial |
| Substitute a Blob path | Narrow opaque release ID and server-owned mapping; no request field becomes a path | An authorized configuration operator can still create an over-broad policy |
| Reuse customer or storage credentials | Inbound authorization tokens ignored; exact storage identity; no keys or SAS | Workload identity compromise lasts until RBAC or identity revocation |
| Mix object versions during streaming | Strong ETag snapshot and `If-Match` on every bounded segment | Deletion or replacement causes a failed transfer and retry |
| Exhaust memory or connections | Configuration/header/object limits, 4 MiB sequential reads, one range, HTTP timeouts, platform quotas | Many valid slow downloads can still consume replicas and egress |
| Extract internal details from errors or logs | Stable bounded responses and redacted structured logs | Platform access logs must follow the same data-minimization policy |
| Inject a dependency or build artifact | Module checksums, pinned tools and images, secret/SAST/SCA gates, exact source/image binding, SBOM, signature and provenance | Upstream compromise before pin review or a compromised CI control plane |
| Publish an unreviewed image | Read-only candidate job, protected release environment, immutable-tag check, digest signing and attestation | A compromised authorized publisher or environment approver |

## Security invariants

- Authentication is necessary but never sufficient for a download.
- Every denial that can be decided locally occurs before any storage operation.
- Customer input never selects an account, container, Blob path, credential, or filename.
- The service obtains only Blob read authority and never returns a storage URL or SAS.
- A response contains bytes from one observed object version or fails.
- Required scanner errors and integrity mismatches fail closed.
- Release tags are promoted only after the exact digest is scanned, signed, and attested.

## Out-of-scope and residual concerns

This service is not digital-rights management and cannot prevent an entitled client from
copying the downloaded file. It does not defend against compromise of the identity provider,
trusted OIDC layer, Azure control-plane owners, GitHub account or repository, or an authorized
customer device. Availability against large distributed attacks is primarily a platform
responsibility. These risks require identity policy, privileged-access controls, network
protections, monitoring, incident response, and an authorized staging penetration test outside
this repository.
