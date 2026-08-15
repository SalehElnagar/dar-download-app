# Threat Model

## Scope and assets

The protected assets are Blob contents, trusted OIDC identity evidence, the Blob reader
identity, image integrity, and security evidence. The live OIDC provider, authentication layer,
Azure control plane, fixed storage container, and deployment are external dependencies owned by
the platform repository.

## Trust boundaries

1. An untrusted external client reaches the approved HTTPS authentication endpoint.
2. The trusted OIDC layer completes or validates the browser-session or bearer flow.
3. One configured adapter maps trusted evidence into exactly one issuer and subject: either a
   stripped generic header pair or a tenant-bound Azure Container Apps principal.
4. The Go service exact-matches the configured issuer and validates one authenticated identity.
5. The service validates two raw ASCII path segments and constructs exactly
   `{version}/{file_name}` inside the configured container.
6. The service obtains a storage token for one dedicated user-assigned identity.
7. Private networking carries ETag-bound reads to the fixed Blob account and container.
8. GitHub Actions converts reviewed source into an immutable signed image and evidence.

The internal identity boundary is safe only when the trusted layer rejects caller values,
prevents duplicates, selects one adapter, and blocks all direct access to application ingress.
The fixed container is the content-audience boundary: every valid authenticated identity can
access every safe two-segment Blob name within it.

## Attacker capabilities and controls

| Abuse path | Primary controls | Residual risk |
| --- | --- | --- |
| Forward a download link | Authentication still occurs before Blob access | Any authenticated user in the accepted population can follow the link, and a recipient can redistribute bytes after download |
| Forge internal identity headers | Header stripping, private ingress, exactly-one checks, bounded values | A compromised or bypassed trusted layer can impersonate users |
| Mix generic and Azure identity modes | Explicit startup mode and cross-mode header rejection | A wrong mode causes denial until configuration is corrected |
| Substitute an Azure tenant or principal | Exact Entra v2 issuer/tenant binding, canonical object ID, duplicate rejection, and equality between claim and platform ID header | A compromised Container Apps auth sidecar remains authoritative inside its boundary |
| Substitute the issuer or vary a trailing slash | HTTPS issuer validation and byte-exact match to startup policy | A misconfigured upstream can deny valid requests or assert the configured issuer incorrectly |
| Reuse the same subject under another issuer | Issuer and subject are validated as one trusted pair | Upstream compromise remains authoritative inside its boundary |
| Smuggle duplicate issuer or subject values | Both header value lists must contain exactly one entry | A non-conforming intermediary that rewrites field semantics can cause denial |
| Traverse or alias a Blob path | Exact route shape, ASCII allowlist, length bounds, dot-only rejection, and rejection of encoding, slash, backslash, query, controls, Unicode, and nested paths | An authenticated caller can access and probe every safe exact name in the fixed container by design |
| Enumerate storage | No list operation; authentication precedes exact Stat/read | Authenticated callers can still guess valid names and distinguish missing objects through bounded 404 responses |
| Treat a filename as authorization | Documentation and deployment contract make the fixed container the audience boundary | Incorrectly placing differently authorized data in the same container exposes it to every authenticated caller |
| Reuse customer or storage credentials | Inbound authorization tokens ignored; exact storage identity; no keys or SAS | Workload identity compromise lasts until RBAC or identity revocation |
| Mix object versions during streaming | Strong ETag snapshot and `If-Match` on every bounded segment | Deletion or replacement causes a failed transfer and retry |
| Exhaust memory or connections | Header/path/object limits, 4 MiB sequential reads, one range, HTTP timeouts, platform quotas | Many valid slow downloads can still consume replicas and egress |
| Extract internal details from errors or logs | Stable bounded responses and redacted structured logs | Platform access logs must follow the same data-minimization policy |
| Inject a dependency or build artifact | Module checksums, pinned tools and images, secret/SAST/SCA gates, exact source/image binding, SBOM, signature and provenance | Upstream compromise before pin review or a compromised CI control plane |
| Publish an unreviewed image | Read-only candidate job, protected release environment, immutable-tag check, digest signing and attestation | A compromised authorized publisher or environment approver |

## Security invariants

- Authentication is necessary and, for a safe exact path in the fixed container, sufficient for
  application access.
- Exactly one trusted-identity adapter is active, and both adapters produce the same internal
  issuer-and-subject authentication input.
- Invalid routes and identity evidence are denied before any storage operation.
- Caller input never selects an account, container, nested path, credential, or response
  filename different from the validated `file_name`; it selects exactly one two-segment Blob.
- The service never lists the container, obtains only Blob read authority, and never returns a
  storage URL or SAS.
- A response contains bytes from one observed object version or fails.
- Required scanner errors and integrity mismatches fail closed.
- Release tags are promoted only after the exact digest is scanned, signed, and attested.

## Out-of-scope and residual concerns

This service is not digital-rights management and cannot prevent an authenticated client from
copying the downloaded file. It intentionally performs no subject-, version-, or file-level
authorization after authentication. It does not defend against compromise of the identity
provider, trusted OIDC layer, Azure control-plane owners, GitHub account or repository, or an
authenticated customer device. Availability against large distributed attacks is primarily a
platform responsibility. These risks require deliberate container audience separation, identity
policy, privileged-access controls, network protections, monitoring, incident response, and an
authorized staging penetration test outside this repository.
