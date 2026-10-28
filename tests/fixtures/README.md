# Test fixtures

Sanitized, deterministic inputs for the parity and protocol tests.

```
chat/       Chat Completions JSON, error envelopes, and free-lane cases
responses/  Responses API JSON fixtures
anthropic/  Anthropic Messages JSON fixtures
sse/        SSE sequences and forced-stream collapse cases
```

Each `.json` file is one `internal/parity.Fixture`: the client request, the mock
upstream reply, the expected local reply, and a subset assertion over the
request the gateway sends upstream. See `docs/parity.md` for the schema.

Fixtures are committed artifacts, not live captures. They must be reviewable
without a network connection and without any real account.

## Rules

- No prompts, responses, tool arguments/results, API keys, cookies, tokens, or
  proxy credentials. Use synthetic values (`test-key`, `Bearer test-token`,
  `proxy.example:8080`).
- No absolute paths, usernames, hostnames, or real IP addresses.
- No raw network captures. Reduce a capture to synthetic content that preserves
  the structure being asserted.
- Do not copy unrelated upstream test data. Upstream's own static prompt/tool
  assets may be referenced from `internal/gateway/` under GPL attribution; they
  are not duplicated here.
- Keep each fixture minimal. One behavior per file, with the expected output
  next to the input when the harness supports it.

## Normalization

Apply these before committing:

| Value | Replace with |
|---|---|
| `ses_…`, `msg_…`, project/request IDs | stable placeholders (`ses_test`, `msg_test_1`, …) |
| timestamps, dates, `Retry-After` | fixed values or placeholders |
| authorization/cookie headers | removed or `Bearer test-token` |
| user text | synthetic text (or a hash if realism is required) |
| SSE IDs/timestamps | placeholders; preserve event order and field names |
| IPs/hostnames | documentation ranges or `example.com` |

Preserve header order/casing and JSON key order where those are part of the
assertion. Normalize values, not structure.

## Adding a fixture

1. Pick the protocol directory (`chat`, `responses`, `anthropic`, `sse`).
2. Name it for the behavior, not the capture date, e.g.
   `chat/tool-call-roundtrip.json` or `sse/post-200-error.sse`.
3. Normalize it per the table above and add the expected result if the runner
   compares input/output pairs.
4. Add or update the test that consumes it. A fixture without a test is dead
   weight.
5. Run the relevant `go test` target and `node scripts/identity-hash.mjs
   --check` before committing.
6. Review the diff for secrets and personal data; fixtures are public.

If a fixture must change because upstream changed, treat it as a golden update:
explain why, and if identity bytes are involved follow the `identity-update`
policy in `docs/parity.md`.
