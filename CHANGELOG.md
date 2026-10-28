# Changelog

All notable changes to this project are documented here.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

This project is a GPL-3.0-or-later fork of
[`opencode2api`](https://github.com/jasonxu114514/opencode2api). The upstream
baseline for the initial entry is `v1.4.5`.

## [Unreleased]

Nothing yet.

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
- Upstream baseline pinned at `v1.4.5`.

### Notes

- No new runtime behavior, gateway features, admin routes, analytics, proxy
  supply, model overrides, or token savers are included in this entry. The
  codebase remains the upstream baseline.
- Phase 0 is a documentation and guardrails milestone; it does not claim
  feature parity beyond upstream or a public release.
