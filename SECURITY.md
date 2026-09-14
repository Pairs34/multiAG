# Security reporting

Do not post a working key, capability URL, raw state file, or private IDE log in a public issue. For credential exposure, revoke the affected router key and generate a replacement.

Use GitHub's private vulnerability reporting if it is enabled for the published repository. Otherwise, contact the repository owner privately before sharing exploit details. Do not assume this source preparation enables GitHub repository settings.

Runtime files belong in a private user directory. The public source packager deliberately excludes local configuration and does not follow symlinks. Its pattern scanner is a release aid, not a guarantee against every possible secret format. Review staged files before publication.
