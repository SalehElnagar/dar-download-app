# Threat Model

## Scope and assets

The protected assets are release ZIPs and Blob versions, recipient names and emails, notification
receipts, the receipt HMAC key, the SendGrid API key, trusted OIDC identity evidence, the Blob
reader and worker identities, Service Bus messages and locks, image integrity, and security
evidence. A custom provider client secret is also an asset, but it exists only in Azure Container
Apps Authentication and the identity provider; it is prohibited from the Go runtime. The live
OIDC provider, authentication layer, Azure control plane, fixed storage containers, queue,
secrets, and deployment are external dependencies owned by the platform workflow.

## Trust boundaries

1. An untrusted external client reaches the approved HTTPS authentication endpoint.
2. The trusted OIDC layer completes or validates the browser-session or bearer flow.
3. One configured adapter maps trusted evidence into exactly one issuer and subject: a stripped
   generic header pair, a tenant-bound Entra Container Apps principal, or a protected Container
   Apps principal from one exact custom OpenID Connect provider.
4. The Go service validates the configured issuer URL and one authenticated identity. Generic
   mode exact-matches request issuer evidence, and Entra mode binds the issuer to its configured
   tenant. Custom Container Apps OIDC mode instead treats the issuer as a deployment-owned trust
   label: the Go service exact-matches provider evidence and validates the platform principal but
   does not receive or revalidate an issuer claim or token. Container Apps Authentication
   provider metadata and authoritative live readback must bind that provider to the configured
   issuer and audience.
5. The service validates two raw ASCII path segments and constructs exactly
   `{version}/{file_name}` inside the configured container.
6. The service obtains a storage token for one dedicated user-assigned identity.
7. Private networking carries ETag-bound reads to the fixed Blob account and container.
8. Azure DevOps checks out one exact GitHub commit and downloads the protected recipient Secure
   File. The Go publisher validates both inputs and writes immutable release, manifest, and batch
   Blob versions before sending PII-free Service Bus messages.
9. The no-ingress worker uses managed identity to receive one Peek-Lock message and read the exact
   referenced Blob versions. A per-recipient HMAC receipt and Blob ETag CAS protect the SendGrid
   effect across duplicate delivery and retries.
10. A separately protected platform promotion workflow signs and deploys immutable image digests.

The internal identity boundary is safe only when the trusted layer rejects caller values,
prevents duplicates, selects one adapter, and blocks all direct access to application ingress.
The fixed container is the content-audience boundary: every valid authenticated identity can
access every safe two-segment Blob name within it.

## Attacker capabilities and controls

| Abuse path | Primary controls | Residual risk |
| --- | --- | --- |
| Forward a download link | Authentication still occurs before Blob access | Any authenticated user in the accepted population can follow the link, and a recipient can redistribute bytes after download |
| Forge internal identity headers | Header stripping, private ingress, exactly-one checks, bounded values | A compromised or bypassed trusted layer can impersonate users |
| Mix trusted identity modes | Explicit startup mode, mode-specific settings, and cross-mode header rejection | A wrong mode causes denial until configuration is corrected |
| Substitute an Azure tenant or principal | Exact Entra v2 issuer/tenant binding, canonical object ID, duplicate rejection, and equality between claim and platform ID header | A compromised Container Apps auth sidecar remains authoritative inside its boundary |
| Substitute a custom provider, principal, or metadata | Exact bounded provider name in config, IDP header, and decoded `auth_typ`; strict canonical Base64 JSON; opaque bounded platform principal ID; authoritative Container Apps Authentication metadata issuer/audience readback | The Go app receives neither an issuer claim nor a token and therefore trusts the platform configuration and auth sidecar inside this boundary |
| Expose or reuse the custom provider client secret | Secret remains in Container Apps auth configuration; no Go setting, image, header, or log accepts it | A compromised platform owner or auth sidecar can use or replace the confidential client credential |
| Substitute the issuer or vary a trailing slash | Bounded HTTPS issuer validation; generic request evidence and Entra tenant policy bind exactly to startup configuration; custom-provider metadata binding is verified at deployment readback | A misconfigured or compromised trusted layer remains authoritative inside its boundary |
| Reuse the same subject under another provider | Generic and Entra modes bind the issuer in request evidence or tenant policy; custom mode exact-matches provider evidence and relies on deployment metadata binding | Upstream compromise remains authoritative inside its boundary |
| Smuggle duplicate identity values | Each adapter requires exactly one of every mandatory identity header and rejects cross-mode evidence | A non-conforming intermediary that rewrites field semantics can cause denial |
| Traverse or alias a Blob path | Exact route shape, ASCII allowlist, length bounds, dot-only rejection, and rejection of encoding, slash, backslash, query, controls, Unicode, and nested paths | An authenticated caller can access and probe every safe exact name in the fixed container by design |
| Enumerate storage | No list operation; authentication precedes exact Stat/read | Authenticated callers can still guess valid names and distinguish missing objects through bounded 404 responses |
| Treat a filename as authorization | Documentation and deployment contract make the fixed container the audience boundary | Incorrectly placing differently authorized data in the same container exposes it to every authenticated caller |
| Reuse customer or storage credentials | Inbound authorization tokens ignored; exact storage identity; no keys or SAS | Workload identity compromise lasts until RBAC or identity revocation |
| Mix object versions during streaming | Strong ETag snapshot and `If-Match` on every bounded segment | Deletion or replacement causes a failed transfer and retry |
| Exhaust memory or connections | Header/path/object limits, 4 MiB sequential reads, one range, HTTP timeouts, platform quotas | Many valid slow downloads can still consume replicas and egress |
| Extract internal details from errors or logs | Stable bounded responses and redacted structured logs | Platform access logs must follow the same data-minimization policy |
| Inject a dependency or build artifact | Module checksums, pinned tools and images, secret/SAST/SCA gates, exact source/image binding, SBOM, signature and provenance | Upstream compromise before pin review or a compromised Azure DevOps control plane |
| Publish an unreviewed release | Read-only validation, protected Azure DevOps environment, exact commit and recipient digest binding, immutable Blob identities, and workload federation | A compromised authorized publisher or environment approver |
| Expose recipient PII in source or Service Bus | Azure DevOps Secure File, private versioned batch Blob, PII-free queue schema, HMAC receipt identity, secret scanning | Authorized pipeline, storage, or SendGrid operators still handle protected recipient data |
| Forge or alter a notification message | Queue sender RBAC, strict canonical schema, exact operation/message identity, PII-free versioned Blob references, and full digest verification before send | Compromise of both the publisher identity and protected Blob write boundary remains authoritative |
| Redeliver or concurrently process a batch | Stable message ID, Peek-Lock renewal, receipt absence/ETag CAS, terminal-state replay, bounded attempts, and DLQ | Service Bus is at-least-once; duplicate transport delivery remains expected |
| Lose the SendGrid response after the provider accepts | `SEND_STARTED` persisted before the call and stale/ambiguous outcomes terminate as `UNKNOWN` instead of blind resend | An operator needs provider event/readback or manual reconciliation to determine mailbox outcome |
| Partially enqueue a multi-batch release | All immutable intent exists before send, current manifest advances only after every send succeeds, exact replay reuses message IDs, and worker receipts suppress repeated effects | No automatic cross-release reconciler exists; operators must replay a failed release before accepting a higher one |

## Security invariants

- Authentication is necessary and, for a safe exact path in the fixed container, sufficient for
  application access.
- Exactly one trusted-identity adapter is active, and all three adapters produce the same internal
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
