# Headroom contract smoke

Status: **observed on the existing local install; pinned version not verified.**

The plan pins `headroom-ai==0.42.0`. The 9Router Headroom service already
running on this laptop reports `0.38.0`, so this smoke records the contract we
can observe without installing, upgrading, or restarting anything.

## Service

```text
GET http://127.0.0.1:8787/health
service=headroom-proxy
status=healthy
version=0.38.0
```

Command line observed for the running service:

```text
headroom.exe proxy --port 8787
```

Do not stop, restart, or upgrade it. The v1 detector must treat a version that
differs from the pin as `version_mismatch` and require an explicit override
rather than silently using it.

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

Status `200`. The response contained the expected fields:

```json
{
  "messages": [
    {
      "content": "line 1: repeated synthetic tool output ...",
      "tool_call_id": "call_fixture",
      "role": "tool"
    }
  ],
  "tokens_before": 3008,
  "tokens_after": 2600,
  "tokens_saved": 408,
  "compression_ratio": 0.8643617021276596,
  "transforms_applied": ["router:text:0.94"],
  "transforms_summary": {"router:text:0.94": 1},
  "ccr_hashes": []
}
```

The adapter needs `messages`, `tokens_before`, `tokens_after`, and
`tokens_saved`; all four are present. Extra fields are informational and must
not be required.

## Gate

Owner decision (2026-10-28): keep the `0.42.0` pin, do not install or upgrade,
and treat the `0.38.0` result as contract evidence only.

- [x] Existing service reachable on loopback.
- [x] `POST /v1/compress` returns the expected core shape.
- [ ] Pinned `0.42.0` smoke: blocked because the local install is `0.38.0` and
  the owner does not want installs or interference with the running service.
- [ ] Version mismatch refusal: implement in the v1 adapter.
- [ ] Down/timeout/non-2xx fail-open: implement and test in the v1 adapter.

The pinned-version gate stays open until the owner chooses to install
`0.42.0`, pin `0.38.0`, or accept the existing service with an explicit
override. Never auto-upgrade an existing install.
