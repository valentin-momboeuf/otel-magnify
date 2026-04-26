# Security Policy

## Supported versions

Until v1.0, only the latest minor release receives security updates.
After v1.0, the two latest minor releases will be supported.

| Version  | Supported       |
|----------|-----------------|
| latest   | ✅ Supported    |
| < latest | ❌ Not supported |

## Reporting a vulnerability

**Do not open a public GitHub issue for a security vulnerability.**

**Preferred channel — GitHub Private Vulnerability Reporting:**
[Report a vulnerability](https://github.com/Magnify-Labs/otel-magnify/security/advisories/new)

Please include:

- Affected version (or commit SHA)
- Steps to reproduce
- Impact assessment (what an attacker can achieve)
- Suggested fix, if any

## Response timeline

| Stage                 | Target                            |
|-----------------------|-----------------------------------|
| Acknowledgement       | within 72 hours                   |
| Initial assessment    | within 7 days                     |
| Patch ETA             | depends on severity (CVSS-based)  |
| Public disclosure     | coordinated, after patched release |

## Scope

**In scope:**

- The `otel-magnify` server (REST API, OpAMP endpoints, WebSocket hub)
- The `sdkagent` binary
- Helm chart and official Docker images published to `ghcr.io/magnify-labs/*`

**Out of scope:**

- Third-party integrations (OTel collectors, agent SDKs not published by this project)
- Self-hosted deployments running with non-default configurations or modified code
- Vulnerabilities in upstream dependencies that are not reachable from `otel-magnify` code paths (use [govulncheck](https://pkg.go.dev/golang.org/x/vuln/cmd/govulncheck) to confirm reachability)

## Disclosure

We follow coordinated disclosure. After a fix lands in a released version,
we will:

1. Publish a GitHub Security Advisory referencing the CVE (if assigned)
2. Update `CHANGELOG.md` with the security fix entry
3. Credit the reporter, unless they request anonymity
