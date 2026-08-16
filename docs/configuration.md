# Runtime Configuration Contract

The process reads only the following application-specific settings. These names are an
intentional breaking POC contract and have no legacy aliases.

| Variable | Required | Contract |
| --- | --- | --- |
| `DAR_DOWNLOAD_TRUSTED_IDENTITY_MODE` | no | `oidc_headers` by default, or the explicitly enabled `azure_container_apps` or `azure_container_apps_oidc` adapter |
| `DAR_DOWNLOAD_OIDC_ISSUER` | yes | Exact HTTPS issuer expected from the trusted OIDC boundary |
| `DAR_DOWNLOAD_OIDC_PROVIDER_NAME` | mode-specific | Exact custom provider name required only by `azure_container_apps_oidc` |
| `DAR_DOWNLOAD_AZURE_CONTAINER_APPS_TENANT_ID` | mode-specific | Canonical tenant UUID required only by `azure_container_apps` |
| `DAR_DOWNLOAD_STORAGE_ACCOUNT_NAME` | yes | Fixed private Azure Blob account name |
| `DAR_DOWNLOAD_STORAGE_CONTAINER` | yes | Fixed private Azure Blob container name |
| `DAR_DOWNLOAD_MANAGED_IDENTITY_CLIENT_ID` | yes | Canonical client UUID for the storage-only managed identity |
| `DAR_DOWNLOAD_PORT` | no | Listener port from 1 through 65535; defaults to `8000` |

The obsolete `DAR_DOWNLOAD_RELEASES_JSON` setting is not ignored: its presence makes startup
fail. Remove it when moving to the dynamic path contract. There is no replacement release map,
subject allowlist, or global allowlist.

The issuer is stored exactly and compared to available trusted evidence without normalization.
It must be an absolute HTTPS URL of at most 2,048 UTF-8 bytes with a non-empty host and no user
information, query, fragment, or control character. A trailing slash is significant. In custom
Container Apps OIDC mode, the principal has no documented issuer field, so deployment readback
must instead prove that the platform provider metadata uses this exact issuer.

The separately deployed authentication layer owns browser and bearer authentication. The app
does not implement an authorization-code flow and does not validate customer session or bearer
tokens. Direct ingress that bypasses the configured trusted layer is forbidden.

## Trusted identity modes

Exactly one mode is active for the process:

- `oidc_headers` is the default provider-neutral mode. The trusted layer must strip caller
  identity assertions and inject exactly one `X-DAR-OIDC-Issuer` and exactly one
  `X-DAR-OIDC-Subject` on private ingress. Any Azure Container Apps principal header makes this
  mode reject the request.
- `azure_container_apps` is an optional deployment adapter. Azure Container Apps Authentication
  must be enabled in front of the app and must require authentication. The app accepts exactly
  one platform-reserved `X-MS-CLIENT-PRINCIPAL` and exactly one
  `X-MS-CLIENT-PRINCIPAL-ID`; if `X-MS-CLIENT-PRINCIPAL-IDP` is present, it must occur exactly
  once with value `aad`. Caller `X-DAR-*` identity assertions make this mode reject the request.
- `azure_container_apps_oidc` is the custom OpenID Connect Container Apps adapter. Container Apps
  Authentication must be the sole ingress authentication layer. The app requires exactly one
  platform-reserved `X-MS-CLIENT-PRINCIPAL`, `X-MS-CLIENT-PRINCIPAL-ID`, and
  `X-MS-CLIENT-PRINCIPAL-IDP`. The IDP header and decoded `auth_typ` must both byte-exact-match
  `DAR_DOWNLOAD_OIDC_PROVIDER_NAME`. Caller `X-DAR-*` assertions, missing or duplicate protected
  headers, and Entra `aad` evidence fail authentication.

Azure mode also requires `DAR_DOWNLOAD_AZURE_CONTAINER_APPS_TENANT_ID`. Both it and the
platform principal/object IDs use lowercase canonical UUID form. The configured issuer must be
exactly `https://login.microsoftonline.com/<tenant-id>/v2.0`, which binds the adapter's tenant to
the same issuer used by the trusted boundary. The custom adapter requires the Azure tenant
variable to be absent, while the two existing modes reject a custom provider-name setting.
Generic mode retains its existing Azure-tenant behavior, and unknown modes fail startup.

Microsoft's current Container Apps documentation requires a unique alphanumeric custom provider
name but its current ARM schema publishes no numeric maximum. This application therefore applies
a conservative 1-to-32-byte ASCII alphanumeric bound. The name is case-sensitive because it is
also the provider alias in the callback and protected principal representation; hyphen,
underscore, dot, whitespace, controls, and Unicode are rejected. The `aad` alias, in any ASCII
case, is reserved for the separate Entra adapter and is rejected to keep the two trust domains
unambiguous.

The Azure principal is strict, standard Base64 JSON. Its four top-level field names and each
claim's `typ` and `val` names are exact and case-sensitive; unknown, missing, duplicate, or
case-variant fields fail authentication. The principal is at most 16 KiB encoded and contains 1
through 64 claims. Claim types and values are bounded to 512 and 4,096 UTF-8 bytes without control
characters. The adapter accepts exactly one tenant claim (`tid` or the mapped tenant-ID URI) and
exactly one object-ID claim (`oid` or the mapped object-identifier URI). Duplicate aliases,
duplicate values, malformed claims, a tenant mismatch, or disagreement between the object-ID
claim and principal-ID header all fail authentication.

The custom-provider principal uses the same strict standard-Base64 exact JSON schema and limits:
at most 16 KiB encoded, exactly four case-sensitive top-level fields, 1 through 64 claims, and
claim names/values bounded to 512/4,096 UTF-8 bytes without controls. The app does not interpret
provider claims or guess a `sub` alias. Microsoft documents `X-MS-CLIENT-PRINCIPAL-ID` as the
provider-set caller identifier, so that protected header becomes the opaque case-sensitive OIDC
subject and must satisfy the 1-to-255-byte subject bound.

In provider-neutral mode, `X-DAR-OIDC-Subject` is an opaque, case-sensitive OIDC subject. The app
accepts one valid UTF-8 value from 1 through 255 bytes with no Unicode control characters. It
performs no UUID parsing, case folding, trimming, or other normalization. Entra mode deliberately
maps a canonical object ID into that same internal subject field, while custom Container Apps
OIDC mode uses the protected platform principal ID verbatim. Once any adapter produces a valid
configured issuer and authenticated subject, the app performs no subject-level authorization.

For `azure_container_apps_oidc`, `DAR_DOWNLOAD_OIDC_ISSUER` is an exact deployment-owned trust
label. The platform configuration and authoritative deployment readback must bind the custom
provider metadata issuer to that exact value. The Go app does not fetch discovery metadata,
independently validate a token, or receive the custom provider's client ID or client secret. The
client secret is a Container Apps secret and is prohibited from Go environment variables,
configuration, request headers, images, and logs.

## Dynamic Blob-path contract

The protected route is:

```text
GET /v1/releases/{version}/download/{file_name}
```

The two raw path segments map deterministically to one Blob in the fixed container:

```text
version=v26.8.31.01, file_name=canton_dars.zip
-> v26.8.31.01/canton_dars.zip
```

The app validates the route, authenticates the caller, constructs `version + "/" + file_name`,
and performs an exact Blob property/read operation. It never lists the container. Authentication
happens before the Blob existence check, so unauthenticated requests cannot use object existence
as a storage oracle.

Both segments allow only ASCII letters, digits, dot, underscore, and hyphen. `version` is 1
through 96 bytes, and `file_name` is 1 through 128 bytes. Empty and dot-only segments are
rejected. Slash, backslash, controls, Unicode, nested paths, queries, malformed escaping, every
percent-encoded representation, and double-encoded path tricks are rejected. The filename is
safe for the emitted attachment header because no quote, backslash, semicolon, control, or
non-ASCII byte is accepted.

Every successfully authenticated identity may access every existing Blob whose name can be
expressed by this exact two-segment grammar. There is no subject-, version-, or file-level
authorization after authentication. Filenames and obscurity are not security boundaries; use a
separate container and deployment boundary for data that must have a different audience. A
missing exact Blob returns the same bounded `release_not_found` response used for an invalid
route without exposing storage internals.

## Fixed parsing and delivery limits

- Storage account names are 3 through 24 lowercase ASCII letters or digits.
- Container names are 3 through 63 lowercase ASCII letters, digits, or hyphens; they start and
  end with a letter or digit and cannot contain consecutive hyphens.
- The storage managed-identity client ID is a lowercase canonical-form UUID string. It grants
  storage authority only and is unrelated to customer identity.
- The HTTP server configures a 32 KiB maximum request-header setting. Its read-header,
  complete-read, write, idle, and graceful-shutdown limits are 5 seconds, 15 seconds, 10 minutes,
  60 seconds, and 30 seconds respectively.
- Range and `If-Range` values are at most 128 bytes. One byte range is supported.
- An object can be at most 256 MiB. The app opens sequential Blob segments of at most 4 MiB with
  one active Blob reader per download. Total request concurrency is a platform-enforced limit.
