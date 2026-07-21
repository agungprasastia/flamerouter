# Security Policy

## Supported versions

This project is under active development. Security fixes are applied on `main`.

## Reporting a vulnerability

Please **do not** open a public GitHub issue for security-sensitive bugs.

1. Email the maintainer associated with the GitHub account that owns this repository, **or**
2. Use GitHub **Private vulnerability reporting** on the repository (if enabled).

Include:

- Description of the issue and impact
- Steps to reproduce or a proof of concept
- Affected version / commit if known

You should receive an acknowledgment when the report is reviewed. Please allow reasonable time for a fix before public disclosure.

## Hardening checklist (operators)

- Change `INITIAL_PASSWORD`, `JWT_SECRET`, `API_KEY_SECRET`, and `MACHINE_ID_SALT` before any network exposure
- Prefer `REQUIRE_API_KEY=true` for shared or tunneled deployments
- Set `AUTH_COOKIE_SECURE=true` behind HTTPS
- Treat OAuth tokens and provider API keys in `DATA_DIR` as high-value secrets (filesystem permissions matter)
- Do not expose the management dashboard to the public internet without auth and TLS
