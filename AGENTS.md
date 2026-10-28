# AGENTS.md

Status: repository instructions for `dev` and `main`; identical on both
branches; feature work targets `dev`. Keep this file accurate; stale
instructions are worse than none.
Version: `0.1.0-dev` · Upstream base: `fe49a021694eb0a2468527d398d049b3bb6b17cf` (`v1.4.5`)

## What this is

A local-first OpenAI/Anthropic-compatible gateway for OpenCode Zen, Zen Go, and
the keyless free lane. It is a GPL-3.0-or-later fork of
`jasonxu114514/opencode2api` with additive usage analytics, proxy supply,
model metadata overrides, and optional token savers.

Not affiliated with OpenCode, Zen, Go, Cloudflare, 9Router, OmniRoute, RTK, or
Headroom.

## Map

```
cmd/opencode2api/       upstream entry point; release artifact is `oc-router`
internal/admin/         admin API, sessions, CSRF, new routes
internal/config/        config schema/defaults/validation, proxy files
internal/gateway/       routing, keys, proxies, identity shaping, WARP
internal/httpx/         Bun uTLS + HTTP/1.1 transport (do not edit)
internal/identity/      session/message IDs (do not edit)
internal/models/        catalog discovery/merge/pricing
internal/protocol/      Chat/Responses/Anthropic bridge and SSE
internal/telemetry/     in-memory metrics and logs
internal/warp/          userspace WireGuard (do not edit)
internal/store/         SQLite, migrations, repositories
internal/usage/         usage recorder/normalizer/pricing/queries
internal/proxyfetch/    source adapters, JUDGE, geo, artifacts, jobs
internal/proxyprobe/    OpenCode lane probe
internal/modelmeta/     metadata resolver, overrides, OpenCode config
internal/saver/         RTK transforms, Headroom client
internal/egress/        P1 egress providers (WARP prefixes, hybrid, remote IPv6, WireGuard)
internal/toolinstall/   uv/pip/python/pnpm/npm detection, installs
internal/contract/      seam interfaces (stdlib only)
internal/ext/           seam implementations and wiring
webui/                  embedded vanilla JS UI
```

Phase 0 contains the upstream baseline; new `internal/` packages are planned and
may not exist yet. Do not create them just to fill the map.

## Where to look

| Need | Start |
|---|---|
| Free-lane breakage | `internal/httpx`, `internal/identity`, `internal/gateway/opencode_prompt.go`, `toolshape.go` |
| Routing/keys/proxies | `internal/gateway/{gateway,upstream,pool,refresh}.go` |
| Config field | `internal/config/config.go`, `config.example.json` |
| Usage | `internal/usage`, `internal/store`, `docs/usage.md` |
| Proxy pipeline | `internal/proxyfetch`, `internal/proxyprobe`, `docs/proxies.md` |
| Model overrides | `internal/modelmeta`, `internal/models`, `docs/models.md` |
| Token savers | `internal/saver`, `docs/token-savers.md` |
| Egress diversity | `internal/egress`, `docs/egress.md` (P1) |
| UI | `webui/js/pages`, `webui/js/components` |
| Upstream sync | `UPSTREAM_BASE`, `PATCHES.md`, `docs/upstream-sync.md` |
| Reference bases | `REFERENCE_BASES.md`, `PROXY_UPSTREAM_BASE` |

## Commands

```sh
go build -o bin/oc-router ./cmd/opencode2api
go run ./cmd/opencode2api -config ./config.json
go vet ./...
gofmt -w cmd internal
go test ./... -race                # Linux/CGO toolchain; see CI for matrix
go test ./...                      # Windows/macOS when race toolchain is absent
go test -tags noext ./...          # parity: extensions off
staticcheck ./...
govulncheck ./...
node --input-type=module --check < webui/js/main.js
```

Parity and fixture commands are documented in `docs/development.md`; if the
name changes, update this file.

## Always

- Preserve identity bytes: TLS ClientHello, header order/casing, UA, session/
  project/message IDs, prompt, tool manifest, WARP.
- Keep upstream-owned **Go/identity** edits ≤30 lines per file per sync,
  marked `// oc-router:` and listed in `PATCHES.md`. Fork-owned product files
  (README, NOTICE, AGENTS, docs, scripts, workflows, ignore/attributes) may be
  replaced wholesale and are listed there too.
- Put new behavior in new packages or behind the four v1.0 seams (plus the P1
  egress selector) in `internal/contract` (implementations in `internal/ext`).
- Add a test or fixture for every behavior change; update goldens
  deliberately.
- When porting from a reference base, update `REFERENCE_BASES.md` and
  `NOTICE`/`PATCHES.md` in the same PR; never copy unlicensed code or a dirty
  working tree.
- Resolve paths from config/OS conventions, never the process CWD.
- Redact secrets, proxy credentials, cookies, and prompts from logs, errors,
  UI, exports, and fixtures.
- Keep usage metadata-only; no prompts/responses/tool results at rest.
- Update docs and `CHANGELOG.md` for user-visible changes.
- Run the finish checklist before every commit.

## Never

- Edit `internal/httpx/bun*.go`, `internal/identity/*`,
  `internal/gateway/opencode_prompt.go`, `toolshape.go`, or `internal/warp/*`
  without an `identity-update` review and updated goldens.
- Hardcode absolute paths, usernames, drive letters, tokens, or API keys.
- Commit `_archive/`, `.agents/`, `.research/`, `.githooks/`, or
  `.main.gitignore` to `main`.
- Store prompts, responses, tool arguments, or raw API keys in SQLite.
- Silently clamp/rewrite a provider context error; OpenCode needs it for
  overflow compaction.
- Install packages (npm/uv/pip) or clone reference repositories without
  explicit owner permission; use the existing 9Router/Headroom installs. If an
  install is approved, use a dedicated venv, never a global install.
- Interfere with running 9Router/Headroom or other local services; use them
  read-only and ask before starting, stopping, or reconfiguring anything.
- Weaken, skip, or delete a failing test to get green.
- Copy code without a compatible license and attribution.
- Use `git add .` or commit without reading the staged diff.
- Tag a release from `dev`, or tag while Guard main/CI is red.
- Bypass the promote strip/guard or weaken the `main` ruleset.

## Branch policy

- `main`: public release; protected; no dev-only paths.
- `dev`: integration; default for work.
- `dev/*`: feature branches.
- `.gitignore` is branch-divergent by design: while on `dev`, edit the shadow
  `.main.gitignore` for rules that must ship on `main`. Promotion copies it over
  `.gitignore` only when it changed, then strips the shadow.
- Promotion default is the local script on a clean tree:
  `node scripts/promote.mjs <version>`.
  It fetches, merges `origin/dev` with `--no-ff --no-commit`, syncs
  `.main.gitignore`, force-strips `_archive`, `.research`, `.githooks`,
  `.main.gitignore`, `.agents`, and the dev instruction files, hard-verifies
  `git ls-files` and `git status`, commits `Merge dev -> main: <version>`,
  pushes `main`, and restores your branch.
- `.github/workflows/promote.yml` is the optional token path. Without
  `PROMOTE_TOKEN` it prints setup instructions and stops. The token is an
  owner fine-grained PAT with `Contents: Read and write` for this repository
  only.
- Never rely on `.gitignore` for tracked files; deletion is explicit. A new
  dev-only file added since the last promote can land silently on merge; the
  pre-push check and `guard-main.yml` are the backstops.
- Guard main red on a promote commit is a hard blocker: strip the paths,
  re-push `main`, and only then tag/release. Never tag past a red guard.
- Tags `vX.Y.Z` on `main` only; never release from `dev`.
- Shipping files must not reference dev-only paths; point at the concept
  instead. Exceptions are this file, `scripts/promote.mjs`, and the
  promote/guard workflows, which must name the paths they enforce.

## Upstream sync

- `git fetch upstream --tags`.
- Create `sync/upstream-<date>` from `dev`; merge `upstream/main` or a tag.
- Resolve conflicts by re-applying small patches or moving behavior to a seam.
  Never accept “ours” wholesale for an upstream-owned file.
- Run `go test -race`, `noext` parity, fixtures, identity hashes, gitleaks,
  and path scan.
- Update `UPSTREAM_BASE`, `PATCHES.md`, `CHANGELOG.md`.
- Record the merge drill time; >2 hours means reduce the patch surface.

## Finish checklist

1. `go test ./... -race` green (or pre-existing failures proven).
2. `go vet`, `gofmt -l`, `staticcheck`, `govulncheck` clean.
3. `go test -tags noext ./...` green.
4. Parity fixtures green; identity hashes unchanged or explicitly updated.
5. gitleaks + path/secret scan clean.
6. `git status` reviewed; only intended files; no scratch.
7. Docs/config example/CHANGELOG updated.
8. No absolute paths or personal data in the diff.
9. Commit message typed/imperative; staged diff read in full.
10. If behavior changed: a test proves it; if not: say why.
11. If promoting: clean tree, local `scripts/promote.mjs`, Guard main green;
    never tag from `dev` or past a red guard.

## Security and privacy

- Admin must bind loopback; the upstream example still uses `0.0.0.0:8081`
  with a placeholder password, so change both before running. No default
  password is acceptable.
- Proxy credentials stay in config; never in DB/artifacts/UI.
- Source URLs are http/https only; block SSRF to loopback/link-local for
  sources (proxy endpoints are explicitly allowed to be private).
- Report vulnerabilities per `SECURITY.md`; do not open a public issue with
  secrets.

## License

GPL-3.0-or-later. Preserve `LICENSE` and `NOTICE`. If you add a dependency or
copy code, check its license and update `NOTICE`.
