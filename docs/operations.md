# Operations and Trusted OIDC Integration

## Responsibility boundary

The Go process is a private authenticated download and streaming adapter. It does not perform
interactive login, redirects, callbacks, session management, discovery, key retrieval, or token
validation. A separately deployed OIDC authentication layer owns the customer browser session
or bearer flow.

After validating the login or token, the deployment selects exactly one trusted-identity mode.
The default `oidc_headers` mode receives one stripped-and-injected `X-DAR-OIDC-Issuer` and
`X-DAR-OIDC-Subject` pair. The optional `azure_container_apps` mode instead accepts only the
platform-reserved Container Apps principal representation and maps its tenant-bound object ID
into the same internal issuer-and-subject model. Only private traffic from the configured layer
may reach the app. The app exact-matches the configured issuer and validates one authenticated
subject before any Blob access.

The platform repository owns the OIDC layer, identity-provider configuration, private ingress,
networking, DNS, Blob account, managed identity, RBAC, monitoring, and deployment revision.
This repository builds and validates only the application image.

## Required platform configuration

Before live testing, the platform owner must prove all of the following:

- The OIDC layer validates the intended issuer and authentication flow before forwarding.
- Protected requests require authentication, while only `/healthz` is intentionally anonymous.
- The selected identity adapter is explicit; cross-mode, caller-supplied, duplicate, malformed,
  oversized, or ambiguous identity evidence is rejected.
- Direct ingress that bypasses the OIDC layer is blocked.
- TLS terminates only at an approved boundary; the app is not exposed directly to the Internet.
- The app receives the exact configured issuer string, including any significant trailing slash.
- No upstream per-user restriction contradicts the application contract that every successfully
  authenticated identity may use every valid path in the configured container.
- The dedicated user-assigned identity has only `Storage Blob Data Reader` on the intended
  container, not account-wide write or key access.
- The app reaches the Blob account through the approved private endpoint and private DNS.
- The storage public network endpoint remains disabled when that is the environment policy.

Authentication is the only application access decision. After one trusted identity is valid,
the Go service performs no subject-, version-, or filename-level authorization.

## Optional Azure Container Apps adapter

`azure_container_apps` is a deployment adapter, not the future customer identity model. Use it
only when Azure Container Apps Authentication is the sole ingress path. Microsoft documents that
this platform middleware runs before the application, manages the authenticated session, injects
identity information, and prevents external requests from setting its reserved identity headers.
See [Container Apps authentication](https://learn.microsoft.com/azure/container-apps/authentication),
[Microsoft Entra configuration](https://learn.microsoft.com/azure/container-apps/authentication-entra),
and [the principal-header format](https://learn.microsoft.com/azure/app-service/configure-authentication-user-identities).

Before enabling this mode, read back and verify all of the following as one deployment unit:

- Container Apps Authentication is enabled, HTTPS is required, and unauthenticated protected
  requests redirect to the single Microsoft provider. Only `/healthz` may be excluded.
- The Entra registration is single-tenant, its redirect URI is exactly
  `https://<container-app-fqdn>/.auth/login/aad/callback`, ID-token issuance is enabled, and the
  auth configuration pins the exact tenant issuer and audience. Token storage stays disabled
  unless separately reviewed.
- The auth configuration has no `allowedPrincipals` identity restriction. The configured tenant
  and audience remain the authentication boundary for the intended single-tenant population.
- `DAR_DOWNLOAD_TRUSTED_IDENTITY_MODE=azure_container_apps`, the canonical tenant setting, and
  the exact issuer agree with the platform auth configuration.
- External ingress belongs to an internal Container Apps environment or an equivalent private
  boundary with no direct route around the authentication sidecar. A caller-supplied generic
  `X-DAR-*` identity pair must be rejected by the app.

Container Apps supports a client-secret-free ID-token flow when no client secret is configured.
Microsoft identifies that as an implicit flow and recommends stronger alternatives where
practical. Treat the zero-credential form as a POC choice: keep the registration dedicated,
single-tenant, token store disabled, and reassess the browser flow before production promotion.

## Blob layout and access

The only protected operation is:

```text
GET /v1/releases/{version}/download/{file_name}
```

After authentication, the validated raw segments select exactly
`{version}/{file_name}` in `DAR_DOWNLOAD_STORAGE_CONTAINER`. For example,
`/v1/releases/v26.8.31.01/download/canton_dars.zip` reads only
`v26.8.31.01/canton_dars.zip`. The app never lists the container and accepts no account,
container, nested-path, or encoded-path input.

Every authenticated identity can access every existing Blob expressible by this safe
two-segment grammar. Filenames are not a security boundary, and an authenticated user may probe
valid names and distinguish a missing object through the bounded 404 response. Put data for a
different audience in a different container and deployment boundary; do not rely on obscure
names.

`DAR_DOWNLOAD_RELEASES_JSON` is retired and must be removed. The process rejects it when present
so an authorization-era deployment cannot silently run with different semantics.

## Runtime constraints

Run the image with:

- the reviewed Linux AMD64 image digest;
- the root filesystem read-only;
- user and group `65532:65532`;
- all Linux capabilities dropped;
- `no-new-privileges` enabled;
- no writable volume unless a future reviewed requirement introduces one;
- bounded CPU, memory, replicas, and platform request concurrency;
- connection draining longer than the largest expected transfer.

The service has bounded request headers, path segments, object size, transfer duration, and
storage segment size. Each download opens at most one Blob reader at a time; total request and
storage concurrency must be bounded by the platform. It supports one HTTP byte range so clients
can resume a download. It streams Blob bytes through the app because the Blob endpoint is
private and no SAS is exposed.

## Health and observability

Use `/healthz` only for liveness. It intentionally does not call the OIDC layer or Blob Storage.
Add a separate platform-level readiness or synthetic download check when those dependencies
must be observed.

Application logs are structured and deliberately exclude subjects, issuers, identity headers,
Blob URLs and paths, request tokens, filenames, and raw errors. Monitor at the platform boundary
for:

- OIDC login, session, token-validation, or header-injection failures;
- elevated `401`, `404`, `416`, or `502` rates;
- transfer latency, cancellation, and revision restarts;
- managed-identity or private-endpoint failures;
- unexpected configuration revision changes.

Use bounded-cardinality dimensions. Do not add subject, version, Blob path, or filename as a
metric or log label.

## Rollout and rollback

Deploy by immutable image digest, not a mutable tag. Start with a non-production revision and a
synthetic Blob using the exact two-segment layout. Verify both trusted identity modes in local or
integration tests, live upstream authentication, wrong-tenant and spoof-header denial, a valid
authenticated missing-object response, full download, resume, byte checksum, private storage
reachability, and audit logs. A second person's live credentials are not required to prove the
application's no-allowlist behavior because deterministic tests cover arbitrary valid subjects.

For rollback, route traffic to the last approved digest or disable the faulty revision. Keep the
prior revision available until readback completes. Never overwrite an existing image tag,
rewrite retained evidence, broaden RBAC, or change storage networking to make a rollout pass.

## Required live assurance

Local ZAP tests cover the app's anonymous and unauthenticated HTTP surface with synthetic route
values. They do not prove live login/token verification, inbound-header stripping, private
ingress, private DNS, managed identity, Blob RBAC, or an authenticated download. Before
production promotion, run a separately authorized staging penetration test with dedicated test
identities and a synthetic Blob. Record the image digest and environment revision in that report.
