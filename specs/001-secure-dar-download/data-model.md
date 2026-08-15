# Data Model: Secure DAR Download Application

The application keeps no database. Its authoritative state is immutable startup policy plus
the current Blob metadata read for each authorized request.

## Release Policy

| Field | Type | Validation | Meaning |
|---|---|---|---|
| `release_id` | string | `dar_` plus 16-96 ASCII letters, digits, `_`, or `-` | Opaque route key; never authority or a Blob path |
| `blob_name` | string | 1-1024 UTF-8 bytes; relative segments; no empty, `.`, `..`, control, query, fragment, or backslash | Server-owned Blob key |
| `download_name` | string | 1-127 safe ASCII characters; must end in `.dar`; no quote or separator | `Content-Disposition` attachment filename |
| `allowed_principal_ids` | set of strings | 1 or more distinct canonical lowercase UUIDs | Exact principals entitled to the release |

Global invariants:

- Between 1 and 32 release policies are required.
- The map key equals `release_id`.
- Blob names and download names are unique across releases.
- Unknown JSON fields are invalid.
- The complete JSON policy is at most 64 KiB.

## Runtime Policy

| Field | Type | Validation | Meaning |
|---|---|---|---|
| `tenant_id` | string | canonical lowercase UUID | Only accepted identity tenant |
| `storage_account_name` | string | 3-24 lowercase letters/digits | Existing Azure Storage account |
| `storage_container` | string | 3-63 lowercase letters/digits/hyphens; valid start/end | Existing private Blob container |
| `managed_identity_client_id` | string | canonical lowercase UUID | Exact user-assigned workload identity |
| `releases` | map | Release Policy invariants | Complete download routing and authorization policy |
| `port` | integer | 1-65535; default 8000 | HTTP listen port |

The policy is constructed once before listening. There is no partial runtime state: any
validation failure terminates startup.

## Authenticated Principal

| Field | Type | Source | Validation |
|---|---|---|---|
| `principal_id` | string | trusted platform principal-ID header and one matching claim | both canonical and exactly equal |
| `tenant_id` | string | one tenant claim | canonical and exactly equal to runtime policy |
| `provider` | string | trusted platform principal payload | exactly `aad`, case-insensitive |

State transitions:

1. Raw hosting headers received.
2. Reject if absent, oversized, malformed, unexpected, ambiguous, or mismatched.
3. Construct one immutable principal.
4. Compare principal ID with the selected release's allowlist.
5. Only an exact match may advance to object metadata lookup.

## Object Snapshot

| Field | Type | Validation | Meaning |
|---|---|---|---|
| `size` | integer | 0-256 MiB | Current authoritative object length |
| `etag` | string | non-empty strong ETag | Version condition for every body segment |

The snapshot is request-local. Every body segment uses `If-Match` with this ETag. A version
change makes the storage read fail; the application never retries against a new ETag within
the same response.

## Byte Range

| Field | Type | Invariant |
|---|---|---|
| `start` | integer | `0 <= start < size` |
| `end` | integer | `start <= end < size` |
| `length` | derived integer | `end - start + 1` |

Selection states:

- **Full**: no Range, or Range ignored because `If-Range` does not exactly match the current
  strong ETag.
- **Partial**: one valid range and absent or exact matching strong `If-Range`.
- **Unsatisfiable**: malformed, multiple, empty, reversed, overlong, empty-object, or outside
  the current size.

## Storage Segment

| Field | Type | Invariant |
|---|---|---|
| `offset` | integer | within selected interval |
| `length` | integer | 1 to 4 MiB and no more than remaining selected bytes |
| `etag` | string | exactly Object Snapshot ETag |

Segments are opened and copied sequentially. No request creates more than one active Blob body
reader.

## Release Candidate

| Field | Type | Invariant |
|---|---|---|
| `source_revision` | immutable identifier | reviewed source tree |
| `module_graph` | checksummed module set | `go.sum` verified |
| `image_digest` | OCI digest | exact post-build and release subject |
| `sbom` | SPDX and CycloneDX documents | generated from exact digest |
| `prebuild_evidence` | gate results | all required gates successful |
| `postbuild_evidence` | gate results | all required gates successful |
| `provenance` | attestation | binds builder, source, and digest |
| `signature` | verification material | verifies exact digest |

Candidate states:

`source` -> `prebuild-passed` -> `image-built` -> `postbuild-passed` -> `signed` -> `releasable`

Any required failure enters `blocked`. There is no transition that skips a state.

## Security Exception

| Field | Type | Validation |
|---|---|---|
| `tool` | string | exact scanner |
| `finding_id` | string | exact stable finding identifier |
| `package_or_rule` | string | narrow match; no wildcard-all |
| `reason` | string | concrete risk rationale |
| `owner` | string | accountable reviewer |
| `approved_on` | date | ISO date |
| `expires_on` | date | future ISO date, maximum 90 days |
| `compensating_controls` | list | non-empty and verifiable |

An expired, broad, incomplete, or unmatched exception is invalid and blocks release.
