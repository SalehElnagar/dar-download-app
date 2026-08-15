# Runtime Configuration Contract

The process accepts only the following application settings. These names are an intentional
breaking POC contract and have no legacy aliases.

| Variable | Required | Contract |
| --- | --- | --- |
| `DAR_DOWNLOAD_OIDC_ISSUER` | yes | Exact HTTPS issuer expected from the trusted OIDC boundary |
| `DAR_DOWNLOAD_STORAGE_ACCOUNT_NAME` | yes | Fixed private Azure Blob account name |
| `DAR_DOWNLOAD_STORAGE_CONTAINER` | yes | Fixed private Azure Blob container name |
| `DAR_DOWNLOAD_MANAGED_IDENTITY_CLIENT_ID` | yes | Canonical client UUID for the storage-only managed identity |
| `DAR_DOWNLOAD_RELEASES_JSON` | yes | Strict release-to-Blob and subject authorization policy |
| `DAR_DOWNLOAD_PORT` | no | Listener port from 1 through 65535; defaults to `8000` |

The issuer is stored and compared exactly. It must be an absolute HTTPS URL of at most 2,048
UTF-8 bytes with a non-empty host and no user information, query, fragment, or control
character. The parser does not normalize it, so a trailing slash is significant.

The separately deployed authentication layer owns browser and bearer authentication. It must
validate the OIDC login or token, strip every caller-supplied identity header, and inject
exactly one `X-DAR-OIDC-Issuer` and exactly one `X-DAR-OIDC-Subject` header on private ingress.
The app does not implement an authorization-code flow and does not validate customer session or
bearer tokens. Direct ingress that bypasses this trusted layer is forbidden.

`X-DAR-OIDC-Subject` is an opaque, case-sensitive OIDC subject. The app accepts one valid UTF-8
value from 1 through 255 bytes with no Unicode control characters. It performs no UUID parsing,
case folding, trimming, or other normalization.

## Release policy

`DAR_DOWNLOAD_RELEASES_JSON` is strict JSON keyed by opaque release ID:

```json
{
  "dar_01JABCDEF0123456789XYZ": {
    "allowed_subjects": [
      "customer:synthetic-001"
    ],
    "blob_name": "releases/2026-08/example.dar",
    "download_name": "example.dar"
  }
}
```

Every release requires at least one unique valid subject. Unknown fields, duplicate JSON keys,
duplicate subjects, unsafe Blob or download names, and oversized policy input fail startup.
Authorization requires an exact issuer match and an exact subject match before any storage
operation. The policy is authorization-sensitive configuration and must not enter logs or the
repository with live customer values.

## Fixed parsing limits

- Storage account names are 3 through 24 lowercase ASCII letters or digits.
- Container names are 3 through 63 lowercase ASCII letters, digits, or hyphens; they start and
  end with a letter or digit and cannot contain consecutive hyphens.
- The storage managed-identity client ID is a lowercase canonical-form UUID string. It grants
  storage authority only and is unrelated to customer identity.
- The complete release policy is at most 64 KiB and contains 1 through 32 releases.
- Release IDs match `^dar_[A-Za-z0-9_-]{16,96}$` exactly.
- Blob names are 1 through 1,024 valid UTF-8 bytes. They are relative, contain no control,
  backslash, query, or fragment characters, and contain no empty, `.` or `..` path segment.
- Download names match `^[A-Za-z0-9][A-Za-z0-9._-]{0,122}[.]dar$` exactly.
- Blob names and download names are each unique across the complete policy.

## Runtime delivery limits

The HTTP server configures a 32 KiB maximum request-header setting. Its read-header,
complete-read, write, idle, and graceful-shutdown limits are 5 seconds, 15 seconds, 10 minutes,
60 seconds, and 30 seconds respectively. Range and `If-Range` values are at most 128 bytes. A
configured object can be at most 256 MiB, and the app opens sequential Blob segments of at most
4 MiB with one active Blob reader per download. Total request concurrency is a platform-enforced
limit.
