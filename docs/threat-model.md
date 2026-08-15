# Threat Model

## Scope and assets

The protected assets are DAR contents, release entitlements, Entra identity evidence, the Blob
reader identity, release policy, image integrity, and security evidence. Azure and Entra control
planes are external dependencies owned by the platform repository.

## Trust boundaries

1. The untrusted customer browser reaches the approved HTTPS platform endpoint.
2. Azure Container Apps Authentication completes Entra OIDC and converts verified identity into
   trusted Easy Auth headers.
3. The Go service validates those headers again and enforces the exact release allowlist.
4. The service obtains a storage token for one dedicated user-assigned identity.
5. Private networking carries ETag-bound reads to the fixed Blob account and container.
6. GitHub Actions converts reviewed source into an immutable signed image and evidence.

The Easy Auth header boundary is safe only when the platform strips caller-supplied identity
headers and blocks direct access to the application ingress.

## Attacker capabilities and controls

| Abuse path | Primary controls | Residual risk |
| --- | --- | --- |
| Forward a download link | Exact authenticated principal allowlist per opaque release | An authorized recipient can redistribute bytes after download |
| Forge or confuse identity headers | Easy Auth boundary, provider/tenant/header agreement, one canonical OID, strict size and claim limits | A compromised or bypassed hosting boundary can impersonate users |
| Substitute a Blob path | Narrow opaque release ID and server-owned mapping; no request field becomes a path | An authorized configuration operator can still create an over-broad policy |
| Reuse customer or storage credentials | Inbound authorization tokens ignored; exact managed identity; no keys or SAS | Workload identity compromise lasts until RBAC or identity revocation |
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

This service is not digital-rights management and cannot prevent an entitled client from copying
the downloaded file. It does not defend against compromise of Entra, Azure control-plane owners,
the GitHub account or repository, or an authorized customer's device. Availability against large
distributed attacks is primarily a platform responsibility. These risks require tenant policy,
privileged-access controls, network protections, monitoring, incident response, and an
authorized staging penetration test outside this repository.
