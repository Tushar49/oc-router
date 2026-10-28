# Contributing

Thanks for helping. This project is a GPL-3.0-or-later fork of
[`opencode2api`](https://github.com/jasonxu114514/opencode2api), currently at
upstream baseline `v1.4.5` (`0.1.0-dev`). It is local-first software, not a
hosted service.

By contributing you agree that your contribution is licensed under
GPL-3.0-or-later and that you have the right to submit it.

## Branches

- `dev` — integration branch and the default target for work.
- `dev/*`, `feat/*`, `fix/*` — short-lived topic branches off `dev`.
- `main` — release branch. Do not target it directly; changes reach it through
  promotion.

Open pull requests against `dev`. Keep one logical change per PR. Squash or
keep commits typed and imperative (`feat:`, `fix:`, `docs:`, `test:`,
`refactor:`, `chore:`, `perf:`, `build:`, `ci:`, `upstream:`).

## Before you start

- Read `AGENTS.md` for the repository map and invariants.
- Check existing issues and PRs to avoid duplicate work.
- For anything touching identity, TLS, headers, prompts, tools, or WARP, open
  an issue first; those areas require explicit review and updated goldens.

## Setup

You need Go (see `go.mod`) and, for the WebUI syntax check, Node 22 or newer.
`staticcheck` and `govulncheck` are optional local tools.

```sh
go build -o bin/oc-router ./cmd/opencode2api
go run ./cmd/opencode2api -config ./config.json
```

Use a local, gitignored `config.json`; never commit it.

## Checks

Run these before opening a PR:

```sh
gofmt -w cmd internal
go vet ./...
go test ./...
```

When the platform has a CGO-capable toolchain, also run:

```sh
go test ./... -race
go test -tags noext ./...
```

Optional but expected where installed:

```sh
staticcheck ./...
govulncheck ./...
node --input-type=module --check < webui/js/main.js
```

See `docs/development.md` for platform notes and the planned fixture/parity
commands.

Add a test or fixture for every behavior change. If a behavior cannot be
tested yet, say why in the PR. Do not weaken, skip, or delete a failing test to
get green; fix the cause or explain the pre-existing failure.

## Upstream sync

`opencode2api` is the only repository we merge from. See
`docs/upstream-sync.md` for the full workflow. In short:

- Sync branches are `sync/upstream-YYYY-MM-DD` off `dev`.
- Merge upstream with `--no-ff`; never resolve an upstream-owned file by
  accepting "ours" wholesale.
- Run tests, the noext build, fixtures, and the identity hash check.
- Update `UPSTREAM_BASE`, `PATCHES.md`, and `CHANGELOG.md` in the same PR.

## Patch budget

Keep the fork mergeable:

- New behavior belongs in new packages, behind the seams in `internal/contract`
  with implementations in `internal/ext`.
- Edits to upstream-owned files are capped at **30 lines per file per sync**,
  marked `// oc-router: <reason>`, and listed in `PATCHES.md`.
- Never edit the identity/TLS/WARP files or the prompt/tool assets without an
  `identity-update` review and updated goldens.
- Do not reformat, rename, or "clean up" upstream code.

If a change cannot fit these rules, propose a seam or an ADR instead.

## Secrets and privacy

- Never commit credentials, tokens, cookies, proxy credentials, databases,
  logs, or real user prompts.
- Use synthetic data and normalized IDs in fixtures.
- Redact secrets in issues, PRs, and logs.
- Report vulnerabilities privately per `SECURITY.md`; do not open a public
  issue with secrets.

## Code and commit quality

- Match the surrounding style and use the standard library where practical.
- No generated filler, copy-pasted code without a compatible license and
  attribution, or unexplained drive-by changes.
- No AI-generated slop: submissions must be understood, tested, and owned by
  the contributor. Reviewers may ask you to explain any hunk.
- Update docs, the config example, and `CHANGELOG.md` for user-visible changes.
- Keep commits free of scratch files and unrelated edits.

## Licensing

The project is GPL-3.0-or-later. Preserve `LICENSE` and `NOTICE`. If you add a
dependency or port code, check its license, record the attribution, and update
`NOTICE` (and `PATCHES.md` for upstream edits) in the same PR. Do not copy code
without a compatible license.
