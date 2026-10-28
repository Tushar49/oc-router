# Security Policy

This project is a local-first gateway. It is not operated as a hosted service,
and no public instance is provided. Security reports are taken seriously and
handled privately.

## Reporting a vulnerability

**Do not open a public issue, discussion, or pull request for a security
problem.** Public reports can expose users before a fix is available.

Use GitHub's private vulnerability reporting:

1. Open the repository's **Security** tab.
2. Choose **Report a vulnerability** (GitHub Security Advisories).
3. Include affected version or commit, platform, reproduction steps, impact,
   and any proposed fix.

If private reporting is unavailable, contact the maintainer through the
private channel listed on the repository profile rather than a public issue.

Do not paste API keys, provider credentials, proxy credentials, cookies,
session tokens, prompts, or unredacted logs into any report. Redact secrets and
replace them with placeholders. If a credential was exposed, rotate it
immediately; do not wait for triage.

### What to expect

- Acknowledgement of the report.
- A private assessment and, where appropriate, a coordinated fix and advisory.
- Credit in the advisory if you want it.

Please allow time for a fix before public disclosure. There is no bug bounty.

## Supported versions

The project is pre-1.0 (`0.1.0-dev`) and tracks upstream `v1.4.5` as its
baseline. Only the current `main` release line and the tip of `dev` receive
security fixes. Older tags, unmerged forks, and locally modified builds are not
supported.

| Version | Supported |
|---|---|
| `main` (current release) | Yes |
| `dev` (integration tip) | Best effort |
| Older tags / local forks | No |

Because this is a pre-1.0 project, fixes may require an upgrade rather than a
backport.

## Threat model (local only)

The intended deployment is a single user running the binary on their own
machine, with the admin interface bound to loopback. The model assumes:

- The host, its user account, and the local filesystem are trusted.
- The admin listener is not exposed to a LAN, WAN, reverse proxy, or tunnel.
- The operator configures proxy sources and provider credentials themselves.
- Third-party upstreams (providers, proxy sources, metadata services) are
  untrusted and may return malicious or malformed data.

Out of scope: a multi-tenant deployment, a shared admin password, running as a
public service, and defending against an attacker who already has code
execution or filesystem access as the same user.

## Credentials and proxy handling

- Provider keys and proxy credentials live in the local configuration file,
  which should be readable only by its owner (`0600`, or the platform
  equivalent).
- Credentials are never written to the usage database, exports, artifacts, or
  the admin UI in clear text. The admin API masks secrets and requires
  re-authentication to reveal them where supported.
- Proxy URLs may contain userinfo. Treat the configuration file, backups, and
  any captured logs as sensitive.
- Do not commit `config.json`, proxy lists, databases, logs, or `.env` files.
  They are gitignored by default; keep it that way.
- A public or untrusted proxy can observe, modify, or inject traffic. TLS
  verification stays enabled; do not reuse provider credentials across
  untrusted proxies.
- Proxy source URLs are restricted to `http`/`https`, and loopback/link-local
  targets are rejected for sources. Proxy endpoints themselves may be private
  by design.

## No secrets in logs, errors, exports, or fixtures

Logs, error messages, metrics, usage records, admin responses, exports, and
test fixtures must not contain:

- provider or proxy credentials, cookies, session tokens, or authorization
  headers;
- prompts, responses, tool arguments, or tool results;
- raw request or response bodies that may embed any of the above.

When reporting a bug, provide redacted excerpts only. When adding fixtures,
use synthetic content and normalized identifiers.

## Scope

In scope: the gateway binary, its configuration handling, admin API and UI,
proxy handling, identity/TLS shaping, local database, and release artifacts.

Out of scope: vulnerabilities in upstream providers or third-party services,
misconfiguration that exposes the admin listener, and issues that require an
already-compromised host.

## Release integrity

Releases are expected to ship checksums and signed artifacts. Verify them
before running a downloaded binary. A release without verifiable signatures
should be treated as untrusted.

## Disclosure policy

Coordinate disclosure through the advisory. Do not publish details, exploits,
or user data before a fix and advisory are available. Reports that include
secrets will be closed and the credentials should be rotated.
