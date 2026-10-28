# Upstream patches

This file enumerates every deliberate change to files owned by the upstream
`opencode2api` project. It exists so each sync can re-apply or retire patches
quickly and so the fork stays mergeable.

## Status

**No upstream Go/identity code patches yet.** Phase 0 is a documentation,
CI, and guardrails scaffold. It does not edit upstream Go, identity, prompt,
tool, WARP, or WebUI code.

It does replace or remove several fork-owned product paths that upstream also
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

## Upstream code patches

None. No `// oc-router:` markers exist yet.
