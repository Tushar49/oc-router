# Upstream sync

`opencode2api` is the only repository we merge. Everything else is a reference
base recorded in `REFERENCE_BASES.md`; it is ported or read, never merged.
The goal is a fork small enough that an upstream fix can be integrated without
hand translation and without losing our additions.

## Remotes and branches

```sh
git remote add upstream https://github.com/jasonxu114514/opencode2api.git
git fetch upstream --tags
```

- `origin` — this repository (use your own remote URL).
- `upstream` — `https://github.com/jasonxu114514/opencode2api.git`.
- `main` — release branch.
- `dev` — integration branch; syncs merge here.
- `sync/upstream-YYYY-MM-DD` — one branch per sync, created from `dev`.
- `feat/*`, `fix/*` — feature branches off `dev`.

Never commit planning material or local credentials to either remote branch.

## UPSTREAM_BASE

The repository-root `UPSTREAM_BASE` file is the authoritative pin:

```
repo=https://github.com/jasonxu114514/opencode2api
commit=<40-hex>
tag=<upstream tag>
date=<YYYY-MM-DD>
```

Update it only in the sync PR that actually merges the new upstream commit.
Never point it at a dirty working tree, a local branch, or an unpushed commit.

## Weekly drift (planned automation, manual until it exists)

The intended drift job fetches `upstream/main`, compares it to
`UPSTREAM_BASE.commit`, and opens or updates a single issue titled
`Upstream drift: <date>` containing:

- `git log --oneline <base>..upstream/main`;
- the changed file list;
- overlap with the entries in `PATCHES.md`;
- a diff artifact.

Until that job lands, run the same steps by hand:

```sh
git fetch upstream --tags
git log --oneline <UPSTREAM_BASE.commit>..upstream/main
git diff --stat <UPSTREAM_BASE.commit>..upstream/main
```

Drift reporting **never auto-merges**. A security-relevant drift is labeled and
handled first.

## Per-sync workflow

Target: under 60 minutes of active work. The merge is a KPI, not a release
gate; the gate is that the drill is completed, documented, and the patch
surface is stable or shrinking.

1. Start from an up-to-date `dev` and create the sync branch:

   ```sh
   git switch dev
   git pull --ff-only
   git switch -c sync/upstream-YYYY-MM-DD
   ```

2. Merge upstream (or a specific tag) without fast-forward:

   ```sh
   git merge --no-ff upstream/main
   ```

3. Resolve conflicts by the patch rules below. Never accept "ours" wholesale
   for an upstream-owned Go or identity file. Fork-owned product files may be
   kept as ours and listed in `PATCHES.md`.

4. Run the checks:

   ```sh
   go build -o bin/oc-router ./cmd/opencode2api
   go vet ./...
   go test ./...
   go test ./... -race      # Linux with CGO; otherwise skip
   go test -tags noext ./...
   ```

   Then run the parity fixtures and identity hash check when the harness is
   available, plus gitleaks and the path/secret scan.

5. Update `UPSTREAM_BASE`, `PATCHES.md`, and `CHANGELOG.md`.

6. Open a PR to `dev` with the upstream commit list and a short risk note.

7. Merge the PR, then promote `dev` to `main` through the normal promotion
   path. Never tag from `dev`.

## Patch and seam rules

- New behavior lives in new packages, behind the seams in `internal/contract`
  (`Middleware`, `ProxyPreference`, `ModelOverlay`, `AdminRoutes`, and the
  post-v1.0 `EgressSelector`), implemented in `internal/ext` and wired from
  `cmd`.
- Upstream-owned **Go/identity** edits are at most 30 lines per file per sync,
  marked `// oc-router: <reason>`, and listed in `PATCHES.md` with file,
  symbol, reason, conflict risk, and upstream alternative. Fork-owned product
  files (README, NOTICE, AGENTS, docs, scripts, workflows, ignore/attributes)
  may be replaced wholesale and are listed there too.
- Prefer a new file in an upstream package over editing a large function.
- Do not reformat upstream code, rename upstream symbols, or make drive-by
  cleanups.
- Our packages may import upstream packages. Upstream may import only
  `internal/contract`; it must never import `internal/ext` or a feature
  package.
- Config additions are optional with defaults that preserve upstream behavior.

### Conflict strategy

| Conflict | Strategy |
|---|---|
| New upstream file | Take upstream as-is |
| Upstream changed a file we patched | Re-apply the small patch; if it no longer fits, move the behavior to a seam |
| Upstream changed identity/TLS/WARP | Take upstream, run parity, update goldens; do not re-apply old constants |
| Upstream changed the config schema | Take upstream; add our fields as optional; migrate old config |
| Upstream changed protocol/SSE | Take upstream; update fixtures; re-run parity |
| Upstream deleted a feature | Follow upstream by default; keep it via a seam only with an ADR |
| Our patch is large | Stop and refactor into a package or seam before merging |

## Identity hashes

The following files and assets must not change silently:

- `internal/httpx/bun*.go`
- `internal/identity/*`
- `internal/gateway/opencode_prompt.go`
- `internal/gateway/toolshape.go`
- embedded prompt/tool assets
- `internal/warp/*`

When the parity harness lands, it will compute a SHA-256 manifest over these
paths. A mismatch fails the run unless the change carries an `identity-update`
review, updated golden fixtures, a note explaining the upstream reason, and
maintainer approval. Until then, treat any diff in these paths as a stop
signal and review it manually.

A merge that changes identity behavior is a minor version bump and gets a
release-note callout.

## Merge drill

Once per release cycle:

1. Pick one real upstream commit, preferring a bug fix.
2. Merge it into `dev` with the workflow above.
3. Record the elapsed time and any conflicts below.
4. If it takes more than 2 hours, reduce the patch surface before the next
   release.

### Drill log

| Date | Upstream commit | Elapsed | Conflicts / notes |
|---|---|---|---|
| — | — | — | No drill run yet (Phase 0) |

## Reference bases (monthly)

`REFERENCE_BASES.md` pins every repository, package, and data service we use.
Review it monthly, and whenever the relevant contract changes:

- check releases and HEAD for proxy-rotation, 9Router, RTK, OmniRoute,
  OpenCode, and Headroom;
- note license changes and new tags;
- update the pin, port the diff, run fixtures, and update `NOTICE` and
  `PATCHES.md` in the same PR;
- never use a dirty working tree as a base.

A missing or renamed repository, or a license change, is called out explicitly
and blocks copying until resolved.

## No auto-merge

Drift detection, reference review, and release automation may open issues or
PRs. They must not merge upstream changes automatically. Every sync and every
reference port is reviewed by a maintainer with the checks above.
