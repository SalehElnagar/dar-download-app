# Repository Migration and Rollback

## Target

`SalehElnagar/dar-download-app` becomes the only writable product repository. It contains the Go
download app, Go worker, Go release publisher, Azure DevOps YAML, release input contract, and
operations documentation.

The former `SalehElnagar/dar-download` repository remains a read-only rollback source until the
new repository completes staging and one approved non-production release.

## Cutover

1. Push the consolidation branch without merging it and create the Azure DevOps CI definition
   against `azure-pipelines/ci.yml` on that branch.
2. Run the exact branch candidate successfully, then replace the GitHub Actions required check
   with the Azure DevOps CI status in the `main` branch ruleset. Do not remove the last working
   required check before its replacement is proven.
3. Merge the reviewed consolidation commit while leaving the existing production release
   pipeline pointed at the old repository.
4. Create the new release-pipeline definition from
   `azure-pipelines/dar-release-distribution.yml` in this repository.
5. Configure the Secure File, variable group, workload-federated service connection, private
   agent pool, and protected environment described in
   [release distribution operations](release-distribution.md).
6. Point a reviewed staging copy of the pipeline variables and protected environment at
   non-production resources. Add a synthetic higher release version and test-only Secure File.
   When deploying the new live worker revision, remove `DAR_MAIL_ALLOWED_RECIPIENTS_JSON`; the
   verified immutable batch replaces that POC-only duplicate allowlist. Keep the old worker
   revision available for rollback until the new revision is healthy.
7. Run the new release pipeline against non-production Storage, Service Bus, worker, SendGrid
   sandbox/test address, and download app.
8. Verify immutable Blob versions, PII-free queue body, terminal receipt, Entra-authenticated
   byte-correct download, and audit logs.
9. Only then repoint the production Azure DevOps pipeline and product-team instructions to the
   new repository.
10. Disable writes and triggers in the old repository. Archive it only after the agreed retention
   window and zero-use verification.

## Rollback

Before the first production release, rollback means leaving the existing pipeline pointed at the
old repository. After cutover, stop new release merges, disable the new trigger, and restore the
last approved old pipeline definition. Do not delete Blob evidence, receipts, Service Bus DLQ
messages, logs, or either Git history during reconciliation.

Repository rollback does not undo an email already accepted by SendGrid or a release already
downloaded. Reconcile those effects from receipts, provider events, and download audit logs.
