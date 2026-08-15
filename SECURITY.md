# Security Policy

## Reporting a vulnerability

Use the private **Security** tab and create a private vulnerability report for this repository.
Do not open a public issue and do not include credentials, customer identifiers, live tenant
configuration, DAR content, or exploit data in ordinary pull requests or logs.

If private vulnerability reporting is not enabled, contact the repository owner through the
approved internal security channel. This repository intentionally does not
publish an unverified email address.

Include the affected revision or image digest, impact, prerequisites, a minimal reproduction
using synthetic data, and any evidence that sensitive data may have been accessed. Do not test
against customer or production systems without written authorization.

## Response targets

These are triage targets, not disclosure promises:

| Severity | Initial triage target | Release response |
| --- | --- | --- |
| Critical | One business day | Block or revoke the affected release path immediately |
| High | Two business days | Block promotion and prepare a corrected immutable image |
| Medium or Low | Five business days | Prioritize with the application owner |

Only the current supported image line receives security fixes. Old tags remain immutable for
evidence; they are revoked from deployment and replaced rather than overwritten.

## Release-blocking policy

A release requires zero known Critical or High findings from all required source, dependency,
secret, container, and dynamic scanners. A scanner error, unavailable database, skipped gate,
or candidate-integrity mismatch is a failure.

This initial repository does not automate security exceptions. `security/exceptions.yaml` must
remain exactly empty and a repository test enforces that state. A future exception mechanism
must be reviewed first and must match one exact finding, identify an accountable owner, describe
compensating controls and justification, and expire within 90 days. Inline scanner ignores,
broad suppressions, and exceptions for secret findings are forbidden.

## Containment and recovery

For a suspected compromise:

1. Stop promotion and disable the affected Container App revision or release mapping.
2. Revoke the workload identity's Blob role if storage authority may be compromised.
3. Preserve image digest, SBOM, attestations, audit logs, and gate evidence.
4. Patch and rebuild from reviewed source; rerun every source and image gate.
5. Publish a new signed immutable version and deliberately roll it out.
6. Complete an authorized impact assessment before restoring access.

Deployment, Entra, storage, and RBAC actions are owned by the platform repository and its
approved operators; this application repository performs none of them.
