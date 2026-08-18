# Security Gates and Evidence

No automated process can establish that an application is “100% secure.” This repository uses
independent, fail-closed controls to reduce known risk and make each release decision auditable.

## Before image construction

`scripts/prebuild.sh` starts with Gitleaks. A secret finding stops the workflow immediately and
cannot be bypassed by an exception. Only after that scan passes does it run:

- exact tool-version validation;
- Go and shell formatting plus ShellCheck;
- module tidiness and checksum verification;
- `go vet`, Staticcheck, and a read-only dependency build;
- complete tests and a 90% core statement-coverage floor;
- the Go race detector and five bounded native fuzz targets;
- the Go vulnerability database and Gosec;
- repository-specific Semgrep rules with positive and negative controls;
- Trivy dependency, secret, and configuration scanning;
- workflow-order checks and a final Gitleaks pass.

Required tools are version-pinned. Downloaded binary assets are checksum-verified. Semgrep scans
tracked and untracked candidate product files before publication while retaining explicit
local-only and repository-metadata exclusions. Semgrep and ZAP run from digest-pinned images when
used by the bootstrap workflow.

## Image construction boundary

`scripts/build-image.sh` refuses to build without a passing marker for the current deterministic
source digest. Any source, test, workflow, policy, or documentation change invalidates that
marker. The Dockerfile uses digest-pinned builder and distroless runtime images, a static binary,
and a non-root identity. The initial release is explicitly built for Linux AMD64; the target
platform is recorded with the image evidence rather than inferred from the developer machine.

## After image construction

`scripts/postbuild.sh` verifies that each image still matches the validated source. Both the
download and worker candidates receive SBOM, vulnerability, platform, non-root, minimal-content,
and ELF architecture checks. The download image additionally receives the read-only runtime smoke,
offline ZAP DAST, and candidate-transfer controls because it owns ingress. The worker has no public
HTTP surface; its Azure, Service Bus, Blob, Key Vault, and SendGrid behavior remains a required
staging integration test.

For the applicable image, the scripts:

- generates SPDX and CycloneDX JSON SBOMs with Syft;
- scans the exact image with Trivy and Grype, blocking High and Critical findings;
- checks the Linux AMD64 image manifest, AMD64 ELF binary, configured user, entrypoint,
  filesystem, shell, and package-manager policy;
- starts the exact image read-only with all capabilities dropped and no new privileges;
- verifies health and unauthenticated denial;
- runs OWASP ZAP API scanning over an internal Docker network with no Internet route, no runtime
  add-on updates, and every ZAP warning or failure treated as release-blocking;
- binds the final evidence marker to the source digest and image ID.

Evidence is local and ignored under `.security/evidence/`. CI retains only successful evidence.
Reports from one source digest or image ID cannot authorize another candidate.

The initial gate accepts no security exceptions. A repository test requires the exception
registry to remain exactly empty, so adding an entry cannot silently change scanner behavior.

## Release protection

Azure DevOps CI checks out without retained Git credentials and retains only successful candidate
evidence. Release publication separately binds the exact GitHub `main` commit and the SHA-256 of
the protected recipient Secure File, then uses a protected Azure DevOps environment, a private
agent, and a workload-federated service connection.

The distribution pipeline can create immutable release evidence and send to one Service Bus
queue; it has no SendGrid or deployment authority. Image signing, registry promotion, and Azure
deployment remain a separate protected platform workflow. The candidate evidence in this
repository must be verified again against the exact promoted digest; a local or CI candidate by
itself is not production authorization.

## Residual assurance

The local DAST target cannot authenticate through a live OIDC layer and intentionally cannot access
live Blob Storage. An authorized staging penetration test remains mandatory before production.
Newly disclosed vulnerabilities also require continuous dependency alerts and rebuilds; a scan
is evidence about its database and candidate at one point in time.
