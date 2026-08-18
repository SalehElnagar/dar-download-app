# Architecture Diagrams

`dar-release-distribution-azure-devops.drawio` is the editable source of truth. It has two pages:

1. End-to-end release publication, notification, authentication, download, and audit flow.
2. Direct Azure DevOps email sending compared with Service Bus and the Go worker.

Generated views:

- `dar-release-distribution-azure-devops.png`
- `dar-release-distribution-why-service-bus.png`
- `dar-release-distribution-end-to-end-animated.drawio.svg`
- `dar-release-distribution-service-bus-comparison-animated.drawio.svg`
- `dar-release-distribution-interactive.html`

The production view keeps recipient PII outside Git in an Azure DevOps Secure File. The mail
simulator and legacy Python POC are intentionally absent from the production architecture.
