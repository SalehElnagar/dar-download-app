# Protected notification recipients

Production recipients are not committed to Git. Store the canonical CSV in Azure DevOps
Library → Secure files, authorize only the release pipeline, and set
`DAR_RECIPIENTS_SECURE_FILE` in the protected non-secret variable group to that Secure File's
name.

Required format:

```text
first_name,last_name,email
```

The current contract accepts ASCII first and last names from 1 through 64 characters using
letters, space, period, apostrophe, or hyphen. Email addresses are normalized to lowercase and
must be unique. Extend the cross-component contract and tests before onboarding names outside
that character set; do not silently transliterate customer data.

Use [notification-recipients.example.csv](notification-recipients.example.csv) only as a schema
example. Azure DevOps downloads the protected file to the agent for each job and deletes that
download when the job finishes. The pipeline binds its SHA-256 across validation and publication
so a changed file cannot silently pass the earlier validation stage.
