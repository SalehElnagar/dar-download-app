# Release input

Add one immutable product release using this exact layout:

```text
releases/
└── v8.31.1.05/
    └── dar-8.31.1.05.zip
```

The folder uses `vA.B.C.DD`; the ZIP omits the leading `v`. The ZIP must contain exactly one
non-empty `.dar` and may also contain bounded `.pdf`, `.docx`, `.md`, or `.txt` guides.

Release ZIPs use Git LFS so packages up to the application's 256 MiB limit are not committed as
ordinary Git blobs. Product contributors need Git LFS installed before adding a release. Azure
DevOps checkout explicitly downloads LFS objects before validation; an unresolved pointer fails
ZIP validation.

The Azure DevOps release pipeline selects the highest canonical folder, validates the complete
ZIP, derives the release manifest, and publishes immutable Blob versions. Do not add a manifest,
recipient file, SAS URL, storage credential, or provider credential here.

A commit under `releases/**` is a distribution request. Protect `main` with review and require
the release pipeline's protected environment approval before publication.
