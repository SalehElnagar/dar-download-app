# Operations and Container Apps Integration

## Responsibility boundary

The Go process is a private download authorization and streaming adapter. It does not perform
interactive OIDC itself. Azure Container Apps Authentication, commonly called Easy Auth, owns
the browser redirect, Entra callback, session, and trusted identity-header injection.

The platform repository owns the Container App, authentication child resource, Entra app
registration, private networking, DNS, Blob account, managed identity, RBAC, monitoring, and
deployment revision. This repository builds and validates only the application image.

## Required platform configuration

Before live testing, the platform owner must prove all of the following:

- Container Apps Authentication uses the intended Microsoft Entra tenant and registration.
- Unauthenticated protected requests redirect to the Entra provider, while only `/healthz` is
  excluded from authentication.
- The callback is the exact Container App callback URI and ID-token issuance is enabled.
- Caller-supplied `X-MS-CLIENT-PRINCIPAL` and `X-MS-CLIENT-PRINCIPAL-ID` values cannot reach the
  app unchanged. Direct ingress that bypasses Easy Auth is blocked.
- TLS terminates only at an approved platform boundary. The app is not exposed directly to the
  Internet.
- The dedicated user-assigned identity has only `Storage Blob Data Reader` on the intended
  container, not account-wide write or key access.
- The Container App reaches the Blob account through the private endpoint and private DNS.
- The storage public network endpoint remains disabled when that is the environment policy.

Authentication proves identity. The Go service still requires the same canonical principal in
the release's exact allowlist before it performs any Blob operation.

## Release policy

`HARMONY_DAR_RELEASES_JSON` is strict JSON keyed by an opaque release ID. An example using only
synthetic identifiers is:

```json
{
  "dar_01JABCDEF0123456789XYZ": {
    "allowed_principal_ids": [
      "33333333-3333-4333-8333-333333333333"
    ],
    "blob_name": "releases/2026-08/example.dar",
    "download_name": "example.dar"
  }
}
```

The policy parser rejects unknown fields, duplicate JSON keys, unsafe names, malformed UUIDs,
empty allowlists, and excessive sizes. Release IDs are sent to customers; Blob paths are never
accepted from a request.

Treat the policy as authorization-sensitive configuration. Supply it through the approved
Container App configuration mechanism, prevent it from entering logs, and review every change.

## Runtime constraints

Run the image with:

- the reviewed Linux AMD64 image digest;
- the root filesystem read-only;
- user and group `65532:65532`;
- all Linux capabilities dropped;
- `no-new-privileges` enabled;
- no writable volume unless a future reviewed requirement introduces one;
- bounded CPU, memory, replicas, and platform request concurrency;
- platform connection draining longer than the largest expected transfer.

The service has bounded request headers, configuration, object size, transfer duration, storage
segment size, and storage concurrency. It supports one HTTP byte range so browsers can resume a
download. It streams Blob bytes through the app because the Blob endpoint is private and no SAS
is exposed.

## Health and observability

Use `/healthz` only for liveness. It intentionally does not call Entra or Blob Storage. Add a
separate platform-level readiness or synthetic download check when those dependencies must be
observed.

Application logs are structured and deliberately exclude principal IDs, identity headers,
release policy, Blob URLs and paths, request tokens, and raw errors. Monitor at the platform
boundary for:

- authentication redirect or callback failures;
- elevated `401`, `403`, `404`, `416`, or `503` rates;
- transfer latency, cancellation, and revision restarts;
- managed-identity or private-endpoint failures;
- unexpected configuration revision changes.

Use bounded-cardinality dimensions. Do not add customer principal, release ID, Blob path, or
download filename as a metric or log label.

## Rollout and rollback

Deploy by immutable image digest, not a mutable tag. Start with a non-production revision and a
synthetic DAR. Verify sign-in, exact entitlement, denial for a second identity, full download,
resume, byte checksum, private storage reachability, and audit logs.

For rollback, route traffic to the last approved digest or disable the faulty revision. For a
bad or over-broad release policy, remove the affected mapping through a reviewed configuration
revision. Never overwrite an existing image tag or rewrite retained evidence.

## Required live assurance

Local ZAP tests cover the app's anonymous and unauthenticated HTTP surface with synthetic data.
They do not prove the Entra redirect, callback, trusted-header boundary, private DNS, managed
identity, Blob RBAC, or authenticated download in a tenant. Before production promotion, run a
separately authorized staging penetration test with dedicated test identities and a synthetic
DAR. Record the image digest and environment revision in that report.
