# Runtime Configuration Contract

The binary reads configuration once before binding its HTTP port. All values except the port
are required. Configuration contains identifiers and policy, never credentials or tokens.

## Environment variables

| Variable | Required | Example | Rules |
|---|---:|---|---|
| `HARMONY_DAR_TENANT_ID` | yes | `11111111-1111-4111-8111-111111111111` | canonical lowercase UUID |
| `HARMONY_DAR_STORAGE_ACCOUNT_NAME` | yes | `stdardownloadpoc01` | 3-24 lowercase letters/digits |
| `HARMONY_DAR_STORAGE_CONTAINER` | yes | `dar-releases` | valid 3-63 character container name |
| `HARMONY_DAR_MANAGED_IDENTITY_CLIENT_ID` | yes | `22222222-2222-4222-8222-222222222222` | canonical lowercase UUID |
| `HARMONY_DAR_RELEASES_JSON` | yes | JSON below | exact schema, at most 64 KiB and 32 releases |
| `HARMONY_PORT` | no | `8000` | integer 1-65535; defaults to 8000 |

Synthetic release-policy example:

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

Each release entry MUST contain exactly the three shown fields. Unknown fields, duplicate
principals, duplicate Blob/download names, non-canonical UUIDs, unsafe names, or empty
allowlists terminate startup.

## Fixed runtime limits

These are security invariants, not operator tuning knobs:

| Limit | Value |
|---|---:|
| Encoded Easy Auth principal header | 16 KiB |
| Claims in Easy Auth payload | 64 |
| Release-policy JSON | 64 KiB |
| Configured releases | 32 |
| Blob name | 1024 UTF-8 bytes |
| Range and If-Range headers | 128 bytes each |
| Complete request headers | 32 KiB |
| Downloadable object | 256 MiB |
| One Blob payload request | 4 MiB |
| Concurrent Blob payload requests per download | 1 |
| Header read deadline | 5 seconds |
| Request read deadline | 15 seconds |
| Response write deadline | 10 minutes |
| Idle connection deadline | 60 seconds |
| Graceful shutdown | 30 seconds |

## Workload identity contract

The Azure client is constructed with `HARMONY_DAR_MANAGED_IDENTITY_CLIENT_ID` and obtains a
token for Azure Storage through the managed identity endpoint. The application has no
configuration option for account keys, connection strings, SAS, customer bearer tokens, or a
public Blob URL. The separate platform deployment must grant only the intended container-level
Blob Data Reader role.

## Trusted hosting contract

The application accepts `X-MS-CLIENT-PRINCIPAL` and `X-MS-CLIENT-PRINCIPAL-ID` only because
Azure Container Apps Authentication is required to authenticate the request and remove any
caller-supplied values before forwarding it. Direct ingress that bypasses that boundary is
unsupported and unsafe. The application still validates provider, tenant, canonical IDs,
claim shape, and per-release entitlement.

## Logging contract

Startup logs may state service version, listen address, and successful configuration counts.
Request logs may state status class, method, route template, duration, and a generated request
correlation value. Logs MUST NOT contain raw identity headers, claims, tokens, tenant or
principal IDs, policy JSON, release IDs, Blob names, storage URLs, filenames, ranges, ETags,
customer content, or error bodies returned by Azure.
