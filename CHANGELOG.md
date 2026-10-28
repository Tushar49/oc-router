# Changelog

All notable changes to this project are documented here.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

This project is a GPL-3.0-or-later fork of
[`opencode2api`](https://github.com/jasonxu114514/opencode2api). The upstream
baseline for the initial entry is `v1.4.5`.

## [Unreleased]

### Added

- Phase 1 golden fixture harness for Chat, Responses, Anthropic, SSE, forced
  stream collapse, and upstream error passthrough, with a local mock upstream.
- Pinned-binary parity runner (`scripts/parity.mjs` + `TestBinaries`) that
  builds the `UPSTREAM_BASE` commit in a detached worktree and compares the
  normalized client and upstream artifacts.
- Tests for auth, model routing, error passthrough, first-event timeouts,
  identity-manifest integrity, proxy parsing, `#pool` cycling, and the
  `/healthz` starting/ready contract.
- `PARITY-MATRIX.md` records the Phase 1 gateway evidence and the P0 rows that
  remain deferred to later phases.
- `main` and `v*` release tags are protected by repository rulesets; the exact
  rules and bypass model are recorded in `docs/main-protection.md`.

### Changed

- Upgraded the local uv tool to `headroom-ai[proxy]==0.42.0` with owner
  approval; the pinned `/v1/compress` contract smoke is now recorded against
  `0.42.0`.
- Updated the CI `govulncheck` pin to `v1.9.0`; `v1.1.4` panics in its SSA
  builder under Go `1.27`.

### Notes

- No runtime gateway behavior changed in this entry; the new code is test,
  tooling, workflow, and documentation only.

## [0.1.0-dev] - 2026-10-28

### Added

- Phase 0 repository scaffold only: security, contribution, changelog, and
  upstream-patch documentation, plus development and upstream-sync guides.
- CI, parity, upstream-drift, reference-drift, promote, guard-main, and release
  workflows; local promotion and reference-drift scripts.
- Identity hash manifest and checker for the read-only free-lane bytes, plus
  the parity fixture directory skeleton.
- Headroom `/v1/compress` smoke evidence against the existing local `0.38.0`
  service; the pinned `0.42.0` gate remains open.
- `staticcheck.conf` and `.gitleaks.toml` for the upstream baseline's known
  style findings and placeholder curl examples.
- Upstream baseline pinned at `v1.4.5`.

### Changed

- Updated `golang.org/x/net` to `v0.60.0`, `x/crypto` to `v0.57.0`, and
  `x/sys` to `v0.48.0`; the module directive is `go 1.26.0`. CI and release
  builds use Go `1.27.x`, and the Docker builder uses `golang:1.27-alpine`.

### Notes

- No new runtime behavior, gateway features, admin routes, analytics, proxy
  supply, model overrides, or token savers are included in this entry. The
  codebase remains the upstream baseline.
- Phase 0 is a documentation and guardrails milestone; it does not claim
  feature parity beyond upstream or a public release.
