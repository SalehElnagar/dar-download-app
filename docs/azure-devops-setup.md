# Azure DevOps Repository Setup

The source remains in the private GitHub repository, while Azure DevOps owns CI/CD execution.

## GitHub repository controls

Protect `main` with pull requests, CODEOWNERS review, conversation resolution, signed commits
where all maintainers can comply, and disabled force-push/deletion. Keep the repository private.
No GitHub Actions workflow is required.

## Azure DevOps connection

Create an Azure DevOps GitHub service connection authorized only for this repository. Create the
CI and release pipelines from the YAML files under `azure-pipelines/` and require the CI result on
pull requests through the configured GitHub/Azure DevOps integration.

Install Git LFS on private agents. The repository tracks canonical release ZIPs through LFS, and
both pipeline definitions request `lfs: true` during checkout.

For Azure publication, use an Azure Resource Manager workload identity federation service
connection. Do not use a PAT, client secret, storage key, Service Bus connection string, or
SendGrid key in pipeline variables.

## Protected inputs and approval

Upload the recipient CSV to Library → Secure files, grant access only to the release pipeline,
and use the protected variable group and environment described in
[release distribution operations](release-distribution.md). Azure DevOps downloads the Secure
File to the agent and deletes the job-local copy when the job completes; the pipeline also binds
its digest across validation and publication.

The publication stage must use a private agent with private DNS/network reachability to Storage
and Service Bus. Do not make private data-plane endpoints public to accommodate a hosted agent.

## Production promotion

The CI pipeline creates candidate evidence but does not authorize or deploy production. Use the
organization's protected ACR image signing and promotion process, deploy immutable digests, and
complete the staging/readback checklist before changing production traffic.
