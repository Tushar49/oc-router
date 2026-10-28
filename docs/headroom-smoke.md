# Headroom contract smoke

Status: **pinned `0.42.0` verified on the local loopback service.**

The plan pins `headroom-ai==0.42.0`. On 2026-10-28 the owner approved stopping
the local `0.38.0` uv tool, upgrading it to `headroom-ai[proxy]==0.42.0`, and
restarting it. No other service was changed.

## Service

```text
GET http://127.0.0.1:8787/health
service=headroom-proxy
status=healthy
ready=true
version=0.42.0
```

Local installation and command line:

```text
uv tool: headroom-ai[proxy] v0.42.0
command: headroom proxy --port 8787
listen:  127.0.0.1:8787 (loopback only)
```

The service remains loopback-only. Do not stop, restart, reconfigure, or
upgrade it without owner approval. The v1 detector must still treat a version
that differs from the pin as `version_mismatch` and require an explicit
override rather than silently using it.

## Request

`POST http://127.0.0.1:8787/v1/compress`

```json
{
  "model": "gpt-4o-mini",
  "messages": [
    {
      "role": "tool",
      "tool_call_id": "call_fixture",
      "content": "line 1: repeated synthetic tool output\nline 2: repeated synthetic tool output\n... (200 lines total)"
    }
  ]
}
```

The content is synthetic. No prompt, response, credential, or real tool result
was sent.

## Response

Status `200`. The response contained the expected core fields:

```json
{
  "messages": [
    {
      "role": "tool",
      "tool_call_id": "call_fixture",
      "content": "line 1: repeated synthetic tool output ..."
    }
  ],
  "tokens_before": 1808,
  "tokens_after": 1808,
  "tokens_saved": 0,
  "compression_ratio": 1.0,
  "transforms_applied": ["router:noop"],
  "transforms_summary": {"router:noop": 1},
  "ccr_hashes": []
}
```

`0.42.0` chose `router:noop` for this synthetic turn and reported no savings;
the exact savings depend on content and the active compressor. The adapter
needs `messages`, `tokens_before`, `tokens_after`, and `tokens_saved`; all four
are present. Extra fields are informational and must not be required.

## Gate

- [x] Pinned `0.42.0` service reachable on loopback.
- [x] `POST /v1/compress` returns the expected core shape.
- [ ] Version mismatch refusal: implement in the v1 adapter.
- [ ] Down/timeout/non-2xx fail-open: implement and test in the v1 adapter.

Never auto-upgrade an existing install. A future version change requires owner
approval and a fresh smoke against the new pin.
