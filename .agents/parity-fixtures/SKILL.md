---
name: parity-fixtures
description: Capture, normalize, store, and verify a request/response/SSE fixture for the gateway, and update identity or protocol goldens only with evidence.
---

# Parity fixtures

Use when behavior changes in protocol bridging, identity shaping, SSE, error
mapping, routing, or model discovery, or when a live transcript must become a
regression test.

## Rules

- Never store real prompts, responses, API keys, cookies, or proxy
  credentials. Synthesize content or replace it with clearly fake text.
- Normalize random IDs and timestamps to placeholders; do not compare them
  literally.
- One fixture = one scenario: request, upstream response, expected client
  response/SSE.
- Prefer a mock upstream over a live call. Live captures are allowed only for
  behavior that cannot be mocked, and must be scrubbed.

## Steps

1. **Define the scenario** in one sentence (protocol, stream, status, lane,
  tool usage).
2. **Capture** the upstream request and response:
   - method/path/headers/body (redact secrets);
   - status and error envelope;
   - SSE events in order, including terminal markers.
3. **Normalize**
   - session/message/request IDs → `ses_FIXTURE`, `msg_FIXTURE`,
     `req_FIXTURE`;
   - timestamps → fixed UTC values;
   - random digests → stable placeholders;
   - model names → a fixture model.
4. **Store** under the fixture directory (repo-relative), with a JSON metadata
   file: scenario, source (mock/live), date, upstream base, expected behavior.
5. **Assert**
   - status and error code/type/message;
   - headers that matter (content-type, terminal behavior);
   - body/schema (not key order unless identity-sensitive);
   - SSE event sequence after normalization;
   - usage fields, if applicable.
6. **Run**
   - the fixture against our binary;
   - for identity/protocol changes, also against the pinned upstream binary
     and diff normalized outputs.
7. **Identity update** (only if upstream legitimately changed identity):
   - require the `identity-update` label;
   - update the hash manifest and goldens;
   - explain the upstream reason in the PR.

## Verification

- Fixture fails before the change and passes after.
- No secret/PII in the fixture (run gitleaks and a manual read).
- `go test -tags noext ./...` still passes.
- Idempotent: running the suite twice yields the same result.

## Failure handling

- If a fixture is flaky, fix the normalization; do not add sleeps/retries that
  hide the flake.
- If the upstream binary and ours differ, capture both and decide which is
  authoritative; upstream code wins unless our extension is deliberately
  changing behavior.
