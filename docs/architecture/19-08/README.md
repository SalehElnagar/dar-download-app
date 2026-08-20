# Architecture Diagrams

`dar-release-distribution-azure-devops.drawio` is the editable source of truth. Its single page
separates three simple flows:

1. Participant coordinators onboard users in IDT and assign the exact `dar_download` role.
2. Azure DevOps uploads the versioned ZIP and sends one notification through SendGrid to the
   Exchange Online distribution group `dar-release-notifications@dtcc.com`.
3. Container Apps Authentication uses Duende OIDC, and the Go app requires `dar_download` before
   it reads and streams the private Blob.

Generated views:

- `dar-release-distribution-azure-devops.png` — lightweight static preview.
- `dar-release-distribution-azure-devops.drawio.png` — PNG with embedded editable diagram data.
- `dar-release-distribution-end-to-end-animated.drawio.svg` — animated operational flow.
- `dar-release-distribution-interactive.html` — self-contained pan, zoom, and search viewer.

The target design intentionally has no recipient file, Service Bus, notification worker,
recipient batches or receipts, application database, or per-recipient notification state.
Distribution-group membership controls notification only; the IDT role controls download access.

This diagram describes the target design. The current Go app still requires implementation and
verification of the `dar_download` role check before this authorization model is production-ready.
