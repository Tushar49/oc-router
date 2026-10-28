# Parity matrix — Phase 1

This is the Phase 1 gateway/protocol slice of the v1 parity contract. `P0`
blocks release. The full product matrix also includes admin, usage, model
metadata, proxy supply, and token saver rows; those are tracked in
`docs/parity.md` and their own phase gates.

Evidence commands:

```sh
go test ./...
go test -tags noext ./...
node scripts/parity.mjs
node scripts/identity-hash.mjs --check
```

## Gateway and protocol

| # | Feature | Pri | Evidence | Status |
|---|---|---|---|---|
| 1 | Config load, comments, strict schema, defaults, validation | P0 | `internal/config/config_test.go` | Green |
| 2 | Config hot reload, atomic save, `.bak`, secret masking | P0 | Upstream code; no Phase 1 fixture | Deferred |
| 3 | Server-key auth, Bearer / `x-api-key`, constant time | P0 | `TestAuth*` | Green |
| 4 | `/v1/models` list shape and supported endpoints | P0 | `TestModelsListShape` | Green (basic; aliases/limits in Phase 3) |
| 5 | Chat Completions JSON + SSE | P0 | `chat/*`, `sse/free-chat*` | Green |
| 6 | Responses JSON + SSE | P0 | `responses/*`, `sse/responses-stream.json` | Green |
| 7 | Anthropic Messages JSON + SSE | P0 | `anthropic/*`, `sse/anthropic-stream.json` | Green |
| 8 | System One passthrough | P0 | `responses/systemone.json` | Green |
| 9 | Cross-protocol bridge | P0 | `chat/cross-anthropic.json`, `anthropic/cross-chat.json` | Partial (Chat↔Anthropic) |
| 10 | Tool call passthrough / shaping | P0 | `chat/tool-call.json`; binary parity for free shaping | Partial |
| 12 | SSE terminals, usage, `[DONE]` | P0 | SSE fixtures + binary parity | Green |
| 13 | Error envelopes and status mapping | P0 | `chat/upstream-error.json`, auth tests | Green |
| 14 | First-event / attempt timeouts | P0 | `internal/gateway/first_event_test.go` | Green (request size limit not yet) |
| 15 | `/healthz` readiness contract | P0 | `TestHealthzStartingAndReady` | Green |

## Routing, keys, proxies

| # | Feature | Pri | Evidence | Status |
|---|---|---|---|---|
| 17 | Zen / Go / anonymous lanes | P0 | `TestRoutingPaidUsesKeyedLane`, `TestRoutingFreeUsesAnonymousLane` | Green |
| 18 | `prefer`, `free_only`, aliases | P0 | `internal/models/catalog_test.go`; alias fixture still pending | Partial |
| 19 | Key pools, affinity, cooldowns, Retry-After | P0 | `TestKeyPoolAffinityCooldownAndRetryAfter` | Green |
| 20 | Anonymous exit walk, `max_anonymous_exits` | P0 | `TestAnonymousCursorCyclesPoolEntries`, `TestMaxAnonymousExitsDefault` | Partial (full walk not yet) |
| 21 | Static proxy schemes | P0 | `TestParseProxyEntry` | Green |
| 22 | `proxyfile` parsing, comments, relative path | P0 | `TestReadProxyFile`, `TestResolveProxyFiles` | Green |
| 23 | `#pool` fresh-tunnel semantics | P0 | `TestAnonymousCursorCyclesPoolEntries`, `TestPoolTransportIsNeverCooledOrEvicted` | Green |
| 24 | Proxy health checks and rebinding | P0 | `TestProxyHealthRestoresAndRebinds` | Green |

## Identity and free-lane shaping

| # | Feature | Pri | Evidence | Status |
|---|---|---|---|---|
| 30 | Bun uTLS ClientHello | P0 | `TestIdentityManifestMatchesTree` | Green (hash-pinned) |
| 31 | Bun HTTP/1.1 header layout | P0 | Identity hash + binary parity headers | Green |
| 32 | UA/version profiles | P0 | Binary parity headers | Green |
| 33 | Session/affinity/project/request headers | P0 | Binary parity headers | Green |
| 34 | `ses_` / `msg_` ID derivation | P0 | Identity hash + stable fixture identities | Green |
| 35 | Official system prompt + `<env>` | P0 | Identity hash + binary parity body | Green |
| 36 | v1/v2 tool manifest | P0 | Identity hash + binary parity body | Green |
| 37 | Protocol-aware system injection | P0 | Binary parity body | Green |

## Not in this slice

Admin sessions/CSRF/security headers, usage analytics, model metadata
overrides, proxy supply jobs, token savers, WARP live smoke, and egress
diversity belong to later phase gates. `PARITY-MATRIX.md` must be extended when
those land.

## Phase 1 gate

- [x] Golden fixtures cover Chat, Responses, Anthropic, SSE, System One, cross
  protocol, tool calls, and error passthrough.
- [x] Local binary vs pinned upstream binary comparison passes.
- [x] Identity hashes match the manifest.
- [x] Auth, routing, errors, first-event, proxy parsing, `#pool`, and `/healthz`
  tests pass.
- [x] Upstream merge drill recorded in `docs/upstream-sync.md` (6.3 s; one
  fork-owned README conflict).
- [ ] Live free-lane smoke: requires credentials/environment.
- [ ] Remaining P0 rows above are either covered by a later phase or explicitly
  deferred.
