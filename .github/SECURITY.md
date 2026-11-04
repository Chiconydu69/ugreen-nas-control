# Security

The plugin runs as root on the NAS: the fan daemon writes to the fan controller, the LED part writes to the LED controller, and the PHP API runs inside Unraid's web server (POSTs need Unraid's CSRF token).

## Report a vulnerability

Do not open a public issue. Use [private vulnerability reporting](https://github.com/ugreen-nas-control/ugreen-nas-control/security/advisories/new) on GitHub. Include the plugin version, what you did and what happened.

## Supported versions

The latest release on `main`. Fixes ship as a new release.

## Scope

In scope: the PHP API and pages, the Go binary, the install and remove scripts in the `.plg`, the release pipeline. Out of scope: Unraid itself, UGREEN firmware and hardware, problems that need root on the NAS already.

Release assets carry SHA256 sums, an SBOM and build attestations; installs check the hash of every downloaded file.
