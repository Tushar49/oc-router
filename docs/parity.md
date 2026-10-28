# Parity

Parity means the fork behaves like the pinned upstream
(`UPSTREAM_BASE`, currently `jasonxu114514/opencode2api` at `v1.4.5`) for the
written v1 P0 contract, except where a documented extension intentionally
changes behavior. It is not a claim that every upstream file is copied
unchanged.

The fork has two different parity obligations:

1. **Semantic parity** — the same request produces the same method/path/query,
   status, error envelope, JSON shape, and SSE event sequence as upstream.
   Extensions must be no-ops when disabled (`-tags noext` must be
   byte-identical to upstream).
2. **Identity parity** — the bytes that make the free lane work (TLS
   ClientHello, HTTP/1.1 header layout, UA, session/project/message IDs,
   system prompt, tool manifest, WARP internals) must not change silently.
   These files are read-only and hash-pinned.

## What gets compared

| Layer | Compared | Notes |
|---|---|---|
| Transport | method, path, query, header names/values/order | Header order and casing are identity, not cosmetics |
| Body | JSON after normalization | IDs/timestamps placeholders; key order preserved |
| Status | status code + error envelope | No silent clamping or rewriting of provider errors |
| SSE | event sequence, terminal markers, usage fields | Normalize IDs/timestamps, not event order |
| ClientHello | JA4 shape (`t13d1713h1`), cipher order, extensions, ALPN | Captured with a local TLS listener, not only a live gate |
| Identity files | SHA-256 per file | `docs/identity-manifest.json` |

## Fixture normalization

Fixtures are committed sanitized artifacts, never raw captures. Normalize:

- request/message/session/project IDs → stable placeholders (`ses_test`,
  `msg_test_1`, …); keep the prefix/shape when the shape is the assertion;
- timestamps, dates, and retry-after values → fixed values or placeholders;
- authorization, cookie, proxy credentials, and any account identifier →
  redacted or removed;
- user content → synthetic text; if realistic content is needed, hash it and
  commit the hash, not the content;
- SSE streams → synthetic content with the same event/field structure;
- absolute paths, usernames, hostnames, and IPs → placeholders.

Do not commit prompts, responses, tool arguments/results, API keys, or
credentials. Upstream's own static prompt/tool assets may be referenced from
`internal/gateway/` under GPL attribution; do not copy unrelated upstream test
data.

## Running the skeleton

The fixture directories and the identity manifest exist now. The parity runner
(mock upstream, upstream binary comparison, normalized diff) is a Phase 1
deliverable and is intentionally not stubbed here.

```sh
# Verify the identity manifest against the working tree.
node scripts/identity-hash.mjs --check

# Regenerate it only for an approved identity change (see below).
node scripts/identity-hash.mjs

# Existing gates that parity work must keep green.
go test ./...
go test -tags noext ./...
go vet ./...
```

`go test -tags noext ./...` is the parity build: extensions register nothing and
the binary must match upstream behavior. A parity run in CI will use the pinned
upstream commit, not `main`, and must not require network access.

## Identity hash policy

`scripts/identity-hash.mjs` hashes the plan/11 §8 read-only files if they exist:
`internal/httpx/bun.go`, `internal/httpx/bun_dial.go`, `internal/identity/**`,
`internal/gateway/opencode_prompt.go`, `internal/gateway/toolshape.go`,
`internal/warp/**`, plus prompt/tool assets discovered from `//go:embed`
directives in those files. Paths are sorted and repo-relative; the manifest
records the upstream tag/commit and a generation date.

A mismatch is a hard failure unless the pull request:

1. carries the `identity-update` label;
2. includes updated golden fixtures for the affected behavior;
3. explains the upstream reason (commit/PR link) in the description;
4. is approved by a reviewer with identity context.

Do not regenerate the manifest to make a failure disappear. Treat a change to
these bytes like a protocol change: reproduce it, capture the new golden, and
review the diff deliberately. An identity change is a minor version bump and a
release-note callout.

## No real credentials

Fixtures, docs, and scripts contain no real credentials, cookies, tokens, proxy
URLs with passwords, account IDs, or personal data. Use synthetic values such as
`test-key`, `Bearer test-token`, and `proxy.example:8080`. If a test needs a
secret-shaped value, generate it in the test at runtime and redact it from
output.
