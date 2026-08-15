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
- the Go race detector and three bounded native fuzz targets;
- the Go vulnerability database and Gosec;
- repository-specific Semgrep rules with positive and negative controls;
- Trivy dependency, secret, and configuration scanning;
- workflow-order checks and a final Gitleaks pass.

Required tools are version-pinned. Downloaded binary assets are checksum-verified. Semgrep and
ZAP run from digest-pinned images when used by the bootstrap workflow.

## Image construction boundary

`scripts/build-image.sh` refuses to build without a passing marker for the current deterministic
source digest. Any source, test, workflow, policy, or documentation change invalidates that
marker. The Dockerfile uses digest-pinned builder and distroless runtime images, a static binary,
and a non-root identity. The initial release is explicitly built for Linux AMD64; the target
platform is recorded with the image evidence rather than inferred from the developer machine.

## After image construction

`scripts/postbuild.sh` verifies that the image still matches the validated source and then:

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

Pull-request CI has read-only repository permission. A tag build also has read-only permission
and exports only the validated image and evidence. A separate `image-release` environment gates
the short publish job that receives package and OIDC permissions.

The publish job refuses existing release and source tags, pushes a source-addressed candidate,
independently cross-checks the transferred source/image/post-build markers, Linux AMD64 image,
non-root user, SBOM digest, repository, revision, tag, and provenance, then signs the exact
registry digest. It attaches signed SPDX and SLSA provenance attestations, verifies the keyless
signature and both attestations, and only then promotes the digest to the version tag.

Cosign keyless signing uses GitHub's short-lived OIDC identity and the public Sigstore trust
root. The transparency record exposes signing metadata such as repository and workflow identity,
but it does not expose source, DARs, tenant values, or image contents. If repository-name
confidentiality becomes a requirement, replace keyless public signing with an approved private
Sigstore or enterprise-managed key design before tagging a release.

## Residual assurance

The local DAST target cannot authenticate through a real tenant and intentionally cannot access
live Blob Storage. An authorized staging penetration test remains mandatory before production.
Newly disclosed vulnerabilities also require continuous dependency alerts and rebuilds; a scan
is evidence about its database and candidate at one point in time.
