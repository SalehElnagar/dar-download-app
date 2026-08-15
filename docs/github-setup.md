# Private GitHub Repository Setup

The intended destination is the private repository `SalehElnagar/dar-download-app`. Repository
creation, first push, rulesets, package settings, and environment approval are owner actions;
the application build does not need cloud or identity-provider credentials.

## Create and publish

From this repository after reviewing the complete candidate:

```bash
gh auth status
gh repo create SalehElnagar/dar-download-app --private --source . --remote origin
git add --all
git commit -S -m "feat: add secure DAR download service"
git push --set-upstream origin main
```

Do not import the Python POC repository or its deployment history. Preserve it separately as the
prototype and live-environment record.

## Required repository controls

Create a default-branch ruleset for `main` with:

- pull requests required;
- at least one independent approval and dismissal of stale approvals;
- CODEOWNERS review required;
- conversation resolution required;
- required check `Security candidate / candidate`;
- branch deletion and force-push disabled;
- signed commits required if all maintainers can comply;
- repository owner included unless the approved emergency process says otherwise.

Create a tag ruleset for `v*` that prevents deletion and update. Restrict tag creation to the
approved release role.

Create the `image-release` environment with required reviewers when the account plan supports
those controls, prevent self-review where available, restrict it to `v*` tags, and do not add
long-lived cloud credentials. The workflow uses GitHub's short-lived OIDC token only for
signing.

Enable every control supported by the account plan: Dependabot alerts and security updates,
secret scanning, push protection, private vulnerability reporting, and dependency review. Keep
the GHCR package private, grant the repository workflow package write access, and enable
immutable package tags if that control is available.

## Signing visibility

The target owner currently resolves as a GitHub user account, so the workflow does not depend on
GitHub Enterprise-only private artifact attestations. Cosign signs the digest, SPDX inventory,
and SLSA provenance using GitHub OIDC and the public Sigstore trust root. Its transparency entry
reveals repository and workflow identity. Review that metadata boundary before the first tag;
use an approved private signing service instead if even the private repository name must remain
confidential.

## First release verification

After an approved semantic version tag completes, verify the GHCR digest, Cosign signature,
SBOM and SLSA provenance attestations, and retained successful evidence. Deploy the immutable
digest through the separate platform repository and complete the authorized staging OIDC,
header-stripping, authorization, checksum, range-resume, and penetration tests before production
promotion.
