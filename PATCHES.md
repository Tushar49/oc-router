# Upstream patches

This file enumerates every deliberate change to files owned by the upstream
`opencode2api` project. It exists so each sync can re-apply or retire patches
quickly and so the fork stays mergeable.

## Status

Phase 2 adds small upstream Go/WebUI patches for the local usage service. No
identity, prompt, tool, TLS, WARP, or free-lane shaping file is patched. The
patches below are intentionally thin seams; behavior lives in the new
`internal/store`, `internal/usage`, and `internal/admin/usage.go` files.

It also replaces or removes several fork-owned product paths that upstream
owns. Those replacements are listed below so an upstream sync can decide
whether to keep ours, re-apply a small patch, or retire the replacement.

## Rules

- New behavior belongs in new packages, behind the seams in
  `internal/contract` with implementations in `internal/ext`.
- An edit to an upstream-owned **Go or identity** file is at most **30 lines
  per file per sync** and is marked `// oc-router: <reason>` (or a
  `// oc-router: begin` / `// oc-router: end` block).
- Fork-owned product files (`README.md`, `NOTICE`, `.gitignore`,
  `.gitattributes`, `AGENTS.md`, `docs/`, `scripts/`, `.github/`) may be
  replaced wholesale; there is no comment marker for Markdown, JSON, or YAML.
- Every replacement or patch is listed below and reviewed during every upstream
  sync. Stale entries are removed.
- Do not reformat, rename, or "clean up" upstream Go code.
- The identity/TLS/WARP files and the embedded prompt/tool assets are not
  patched without an `identity-update` review and updated goldens.

## Fork-owned replacements (Phase 0)

### README.md

- Replaced the upstream README with the OC Router Phase 0 README.
  Reason: project identity, scope, build, security, and license boundary.
  Conflict risk: high; always take our README on sync and re-check upstream
  setup details manually.
  Upstream alternative: none; keep the upstream build/run facts in sync.

### .github/workflows/release.yml

- Replaced the upstream release workflow with the `oc-router` build, GHCR,
  cosign, SBOM, and GitHub Release pipeline.
  Reason: release artifacts and signing policy differ from upstream.
  Conflict risk: high; take ours and re-check upstream action pins.
  Upstream alternative: none; keep the build command and Go version aligned.

### .github/workflows/checks.yml

- Removed the upstream `checks.yml`; `ci.yml` and `parity.yml` supersede it.
  Reason: one CI entry point plus a focused identity/parity gate.
  Conflict risk: medium; do not re-add upstream's duplicate workflow.
  Upstream alternative: none.

### .gitignore

- Added local-only `_archive/`, `.research/`, `.githooks/`, `bin/`, and `dist/`
  entries. `main` uses `.main.gitignore` via promotion.
  Reason: dev-only material must never be tracked on `main`.
  Conflict risk: high; resolve via `.main.gitignore` and the promote script.
  Upstream alternative: none.

### .gitattributes

- Pinned LF for text and scripts, CRLF for PowerShell.
  Reason: cross-platform line-ending stability and clean diffs.
  Conflict risk: low.
  Upstream alternative: none.

### Dockerfile

- Updated the builder image to `golang:1.27-alpine` to match the Go 1.26 module
  directive and the CI toolchain.
  Reason: the old `1.24` builder cannot build the updated module.
  Conflict risk: low; keep the image at or above `go.mod`.
  Upstream alternative: upstream toolchain bump.

## Dependency patches

### go.mod / go.sum

- Updated `golang.org/x/net` to `v0.60.0`, `x/crypto` to `v0.57.0`, and
  `x/sys` to `v0.49.0`; the module directive is now `go 1.26.0`.
  Reason: `x/net v0.42.0` has GO-2026-6617 and the Go 1.24 standard library has
  advisories; the owner approved the security update. `x/sys` v0.49.0 is the
  transitive requirement of `modernc.org/sqlite`.
  Conflict risk: medium; re-apply security updates on sync.
  Upstream alternative: take the upstream dependency bump when available.
- Added `modernc.org/sqlite v1.61.0` (pure Go, BSD-3-Clause; SQLite itself is
  public domain) for the Phase 2 usage store. This is the only build-tag-free
  option that works with `CGO_ENABLED=0` release builds.
  Conflict risk: low; keep the version current.
  Upstream alternative: none; upstream has no SQLite dependency.

## Upstream code patches

### `internal/config/config.go`

- Added the `Usage UsageConfig` field, the default in `Load`, and the
  `cfg.Usage.Normalize()` call. The type and validation live in the new
  fork-owned `internal/config/usage.go`.
  Reason: expose the Phase 2 usage database settings without growing the
  upstream file beyond the 30-line seam.
  Conflict risk: low; re-apply the field, default and validation call if
  upstream reworks defaults.

### `internal/gateway/runtime.go`

- Added the `usage` field and `SetUsage`, wrapped `dynamic` inside
  `telemetry.Middleware` with the usage middleware, and added `Price` for the
  metadata registry. Each added block is marked `// oc-router:`.
  Reason: capture `RequestMeta` after the gateway finishes without touching
  identity or routing code.
  Conflict risk: medium; keep the wrapper inside telemetry and the field
  initialized before the listener starts.

### `cmd/opencode2api/main.go`

- Creates the usage service after `RuntimeManager`, attaches it with
  `manager.SetUsage`, flushes it on shutdown, and passes it to the admin server.
  Reason: own the database lifecycle in the process entry point.
  Conflict risk: low; keep the service close before `manager.Shutdown`.

### `internal/admin/server.go`

- Added the `usage` field, `SetUsage`, five routes, and `Usage: current.Usage`
  when a config update is assembled.
  Reason: preserve the usage settings on unrelated config saves and expose the
  read-only usage API.
  Conflict risk: low; keep the `current.Usage` copy if upstream changes
  `handlePutConfig`.

### `webui/js/main.js`

- Imports the new `usage-history` page and adds it to `PAGES`. The page module
  inserts its own nav item and section, so `index.html` is unchanged.
  Reason: mount the Phase 2 history view with a two-line upstream edit.
  Conflict risk: low; keep the page in `PAGES` and the dynamic nav insertion
  before main.js binds the nav click handlers.

### `webui/js/i18n.js`

- Added English labels for `nav.usage-history` and `page.usage-history`.
  Reason: the page header and nav item use the existing translation mechanism.
  Conflict risk: low; other locales fall back to the authored Chinese copy.
