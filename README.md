# OC Router

A local-first OpenAI/Anthropic-compatible gateway for OpenCode Zen, Zen Go, and
the keyless free lane. It is a GPL-3.0-or-later fork of
[`opencode2api`](https://github.com/jasonxu114514/opencode2api), extended with
local usage analytics, proxy supply, model metadata overrides, and optional
token savers.

**Status: Phase 0 scaffold.** The gateway baseline is the pinned upstream
release; the extensions described in the plan are not implemented yet. Do not
treat this repository as a finished product.

Not affiliated with OpenCode, Zen, Go, Cloudflare, 9Router, OmniRoute, RTK, or
Headroom.

## Baseline

- Upstream: `v1.4.5` / `fe49a021694eb0a2468527d398d049b3bb6b17cf`
- Upstream license: GPL-3.0-or-later
- Project license: GPL-3.0-or-later (see `LICENSE` and `NOTICE`)
- Go: 1.24 or newer

## Build

```sh
go build -trimpath -o bin/oc-router ./cmd/opencode2api
go vet ./...
go test ./...
```

The upstream binary name and package path are retained during Phase 0 to keep
upstream merges small. Release artifacts use the `oc-router` name.

## Run

```sh
cp config.example.json config.json
# edit config.json: set server_keys and admin credentials
go run ./cmd/opencode2api -config ./config.json
```

The gateway example binds to `127.0.0.1:8080`. The admin example still uses
`0.0.0.0:8081` and a placeholder password; set the admin listener to loopback
and use a real password before running. Check `config.example.json` and the
upstream docs before exposing anything.

## Documentation

- `REFERENCE_BASES.md` — pinned upstream/reference commits and review cadence.
- `UPSTREAM_BASE` — authoritative upstream pin for the fork.
- `PROXY_UPSTREAM_BASE` — proxy-rotation pin and license note.
- `docs/development.md` — local build, test, and platform notes.
- `docs/parity.md` — semantic and identity parity policy.
- `docs/headroom-smoke.md` — observed `/v1/compress` contract and version gate.
- `docs/openapi.yaml` — admin API seed.
- `docs/upstream-sync.md` — sync/merge procedure.
- `AGENTS.md` — repository instructions for contributors and agents.
- `SECURITY.md` — vulnerability reporting.
- `CONTRIBUTING.md` — development and branch rules.

## License and attribution

GPL-3.0-or-later. See `LICENSE` and `NOTICE`. If you redistribute binaries,
publish the corresponding source and preserve the notices.

This project is a local, single-user tool. Users are responsible for their own
provider accounts, proxies, and compliance with provider terms.
