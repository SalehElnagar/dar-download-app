# Contributing

Keep changes within the application boundary. Do not add Azure or Entra provisioning, live
tenant values, customer data, DAR files, deployment state, credentials, or generated security
evidence.

For behavior changes:

1. Add the smallest failing test that proves the desired behavior or regression.
2. Implement the change and keep authorization denials before storage access.
3. Run `make prebuild` against the final source.
4. For packaging or release-path changes, run `make image postbuild` against the exact image.
5. Explain compatibility, operational impact, residual risk, and rollback in the pull request.

Never weaken a gate to make a change pass. Remediate the finding. Automated exceptions are not
supported in the initial repository, and secret findings can never be excepted.
