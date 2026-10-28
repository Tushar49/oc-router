---
name: upstream-sync
description: Merge an upstream opencode2api commit, tag, or PR into the dev branch of this fork, resolve conflicts by patch/seam rules, run parity and security checks, and update UPSTREAM_BASE, PATCHES.md, and CHANGELOG.
---

# Upstream sync

Use this skill when the user asks to “sync upstream”, “merge upstream”,
“port PR #N”, or when the weekly drift issue is being worked.

## Inputs

- Target: a tag (preferred), a commit, or a PR number.
- Current branch must be clean; you are on a `sync/upstream-<date>` branch cut
  from `dev`.
- `UPSTREAM_BASE` and `PATCHES.md` are up to date for the current base.

## Steps

1. **Fetch**
   ```sh
   git fetch upstream --tags
   git switch -c sync/upstream-<date> dev
   ```
2. **Inspect**
   - `git log --oneline <base>..<target>`
   - `git diff --stat <base>..<target>`
   - Overlap with `PATCHES.md` files.
   - If the target changes `internal/httpx/bun*`, `internal/identity/*`,
     `internal/gateway/opencode_prompt.go`, `toolshape.go`, `internal/warp/*`,
     or embedded assets, mark this as an identity-affecting sync.
3. **Merge**
   ```sh
   git merge --no-ff <target>
   ```
   Resolve conflicts:
   - Our patch is small: re-apply on top of upstream.
   - Upstream refactored: prefer upstream; re-implement via a seam.
   - Identity/TLS/WARP changed: take upstream, run parity, update goldens only
     with an `identity-update` review.
   - Never accept “ours” wholesale for an upstream-owned file.
4. **Patch audit**
   - Every remaining `// oc-router:` block is ≤30 lines and listed in
     `PATCHES.md`.
   - Remove stale entries; add new ones with conflict risk.
5. **Verify**
   ```sh
   go vet ./...
   gofmt -l .
   go test ./... -race   # Linux/CGO; otherwise go test ./...
   go test -tags noext ./...
   # parity fixtures / identity hash check
   # gitleaks detect --redact
   # path/secret scan
   ```
6. **Update records**
   - `UPSTREAM_BASE` = target commit/tag/date.
   - `CHANGELOG.md` under “Upstream”.
   - `docs/upstream-sync.md` merge drill time and conflicts.
7. **PR**
   - Title: `upstream: <tag> (<n> commits)`.
   - Body: commit list, behavior changes, conflicts, test evidence, identity
     impact.
   - Target `dev`; do not tag or promote in the same PR.
8. **Merge drill**
   - If this is the release-cycle drill, record elapsed time. If >2 hours,
     open a follow-up to reduce the patch surface.

## Verification

- `go test -race` and `noext` green.
- Parity fixtures green; identity hashes unchanged unless explicitly updated.
- gitleaks and path scan clean.
- `PATCHES.md` and `UPSTREAM_BASE` accurate.
- No secrets or absolute paths in the diff.

## Failure handling

- If identity goldens fail and upstream did not change identity code, suspect
  an accidental patch: revert the suspect edit and re-run.
- If an upstream refactor makes a patch impossible, stop and propose a seam
  change in the PR; do not force a large patch.
- If tests fail for reasons unrelated to the merge, prove they pre-date the
  branch and document it; do not weaken them.
