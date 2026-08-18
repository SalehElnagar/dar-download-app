# Release Distribution Operations

## Responsibilities

| Component | Responsibility | Must not contain or do |
| --- | --- | --- |
| Private GitHub repository | Go source and canonical release ZIP | Secrets or live recipient CSV |
| Azure DevOps Secure File | Current recipient names and emails | Cloud credentials |
| Go release publisher | Validate, derive manifest, publish immutable Blobs, enqueue references | Send email or receive SendGrid authority |
| Service Bus | Durable PII-free batch-reference handoff | Raw recipient names or emails |
| Go worker | Verify exact Blob versions, send email, persist receipt | Query a customer database |
| Go download app | Validate trusted identity and stream exact private Blob | Send email or receive a recipient list |

## Azure DevOps objects

Create these outside Git:

- Pipeline `azure-pipelines/ci.yml` for source and image-candidate validation.
- Pipeline `azure-pipelines/dar-release-distribution.yml` for release publication.
- Secure File containing the canonical production recipient CSV.
- Variable group `dar-release-distribution-production-nonsecret`.
- Azure Resource Manager service connection using workload identity federation.
- Private agent pool with private DNS/network access to Storage and Service Bus.
- Protected environment `dar-release-distribution-production` with the required approval.

The non-secret variable group supplies:

| Variable | Meaning |
| --- | --- |
| `DAR_RECIPIENTS_SECURE_FILE` | Secure File name, not its contents |
| `DAR_RELEASE_ID` | Stable lowercase release-family identifier |
| `DAR_PRIVATE_AGENT_POOL` | Private production publication pool |
| `DAR_AZURE_WORKLOAD_IDENTITY_SERVICE_CONNECTION` | Workload-federated service connection name |
| `DAR_STORAGE_ACCOUNT_NAME` | Private storage account |
| `DAR_RELEASES_CONTAINER` | Immutable release container |
| `DAR_MANIFESTS_CONTAINER` | Release manifest container |
| `DAR_BATCHES_CONTAINER` | Protected recipient-batch container |
| `DAR_SERVICEBUS_NAMESPACE` | Fully qualified Service Bus namespace |
| `DAR_SERVICEBUS_QUEUE` | Notification queue |
| `DAR_APPLICATION_ORIGIN` | Exact approved HTTPS download-app origin |

The service connection needs only the publication actions: create/read the three publisher Blob
containers and send to the one Service Bus queue. It has no SendGrid, Key Vault, Container Apps,
role-assignment, or subscription-owner authority.

## Release procedure

1. Update the protected recipient Secure File through its approved owner workflow.
2. Add exactly one higher release folder and matching ZIP under `releases/`.
3. Review and merge the commit to `main`.
4. Validation binds the exact Git commit, Go contracts, full ZIP, recipient schema, and recipient
   SHA-256.
5. Approve the protected publication environment after reviewing the validated inputs.
6. The private agent rechecks the exact commit and recipient digest.
7. The Go publisher creates immutable identity, release, manifest, and batch Blob versions,
   sends PII-free Service Bus messages, and advances the current manifest with compare-and-set
   only after every batch send returns successfully.
8. The worker verifies every referenced version and digest before any SendGrid request.

The publisher limits each message to 10 recipients. At the worker's maximum 30-second provider
timeout, this leaves approximately ten minutes of headroom inside the 15-minute message-lock
renewal budget; accepted recipients are skipped safely on a replay.

A recipient-only Secure File update does not send email. It becomes the cohort used by the next
release commit. Cross-release notification reconciliation remains a separate backlog feature.

Do not merge a higher release while an earlier publication run is failed. Replay the exact failed
run first. A replay reuses the same message IDs and immutable intent; duplicate messages remain
safe because the worker's receipt CAS is the business-effect idempotency boundary.

## Result interpretation

- Receipt `ACCEPTED` with provider HTTP `202` means SendGrid accepted the request; it is not
  mailbox-delivery proof.
- Receipt `FAILED` is a known permanent result.
- Receipt `UNKNOWN` is an ambiguous provider outcome and must not be blindly resent.
- `dar.download.stream_completed` is the authoritative application event for a successful
  authenticated stream. Resolve its opaque subject through the protected Entra directory view.
- Entra authentication failures happen before the app and require Entra sign-in logs; they are
  not automatically attributable to a release version.

Configure the SendGrid Event Webhook before claiming automated delivered, bounced, or dropped
mailbox status.

## Production worker deployment

Use `Dockerfile.worker`; do not deploy a mail-stub image. Live mode receives the SendGrid key and
receipt HMAC key only through approved runtime secret references. The verified immutable batch is
the live recipient scope, so `DAR_MAIL_ALLOWED_RECIPIENTS_JSON` must be absent in live mode. That
allowlist remains mandatory for stub and SendGrid sandbox testing.
