# Reference bases

Status: **repository reference register**. Last reviewed: 2026-10-28.

This is the single register for every repository or service whose behavior,
code, filters, or metadata we use. It answers three questions:

1. What exactly are we pinned to?
2. What did we take from it?
3. When and how do we review it again?

`UPSTREAM_BASE` remains the authoritative pin for the `opencode2api` fork. This
file covers that base plus every reference-only source.

## 1. Pinned bases

| Base | Pin | License | What we use | Mode | Review |
|---|---|---|---|---|---|
| [opencode2api](https://github.com/jasonxu114514/opencode2api) | `v1.4.5` / `fe49a021694eb0a2468527d398d049b3bb6b17cf` (2026-10-26) | GPL-3.0-or-later | Core gateway, identity/TLS/HTTP shaping, prompt/tools, WARP, proxy pools | **Fork + merge** | Weekly drift + sync PR; merge drill per release |
| [proxy-rotation](https://github.com/Tushar49/proxy-rotation) | `origin/proxyrotation-dev` / `274678424b998a0c0836c22f471828447cd70beb` (2026-10-27) | MIT **owner-declared; LICENSE present in the owner's worktree (2026-10-28), not yet pushed; verify before copying** | Source → fetch → judge → probe contract, artifacts, geo output | **Port to Go; no Node runtime** | Monthly + whenever the contract changes; LICENSE gate in Phase 0 |
| [9Router](https://github.com/decolua/9router) | `v0.5.111` / `3a978a631ad65529c06e48ce495092d5bba29f46` (2026-10-26) | MIT | RTK integration shape, proxy pools/UI patterns, OpenCode free-lane behavior | **Reference/port** | Monthly + when touching token savers or proxies |
| [RTK](https://github.com/rtk-ai/rtk) | `v1.1.0` / `753a3b0e939f2e4490fc24e3a1984010f40a103c` (2026-10-25) | Apache-2.0 | 11 compression filters, autodetect, fail-open behavior | **Go port** | Monthly + when filters or fixtures change |
| [OmniRoute](https://github.com/diegosouzapw/OmniRoute) | `release/v3.8.53` / `6fd55ac39dcfc607ed83f530771320559a0f8a2f` (2026-10-21; package `3.8.53`, nearest tag `v3.8.52`) | MIT | Proxy/UI patterns, OpenCode integration, relay implementation as a **negative** reference | **Reference only** | Monthly |
| [OpenCode](https://github.com/anomalyco/opencode) | `dev` / `6806c6e29a7d2087335794d8b86c6031cdb5f6dd` (2026-10-24; package `2.1.4`) | MIT | Provider config, `models.dev` behavior, compaction semantics, tool/config shape | **Reference + fixtures** | Monthly + release watch |
| [Headroom](https://github.com/headroomlabs-ai/headroom) | `v0.42.0` / `17d38e5699b7d7c69ac6addcd762f39e42dd7f00` (2026-10-26) | Apache-2.0 | Optional `/v1/compress` service | **Runtime dependency, pinned** | Monthly release check; upgrade only after contract smoke |
| models.dev | `https://models.dev/api.json`; observed 2026-10-28 | Service data; review terms | Pricing, capabilities, context/output metadata | **Runtime data** | Refresh 24 h; schema validation; keep last good |
| OpenCode metadata | `https://models.opencode.ai/api.json`; observed 2026-10-28 | Service data; review terms | Zen/Go limits, capabilities | **Runtime data** | Refresh 24 h; schema validation; keep last good |
| Cloudflare WARP | `https://api.cloudflareclient.com/v0i1909051800`; behavior observed 2026-10-28 | Cloudflare terms | Registration and egress behavior | **Behavior only; no code** | Canary + monthly quota/egress check |

### What “pin” means

- For Git repositories: the exact commit, plus the tag/branch that contains it.
- For runtime packages: the exact version and commit (Headroom).
- For data APIs: the date observed and the refresh/schema policy; these are not
  Git commits.
- A **dirty local working tree is never a base**. The opencode2api checkout has
  local README/proxy changes, and proxy-rotation has untracked planning files;
  neither is part of the pins above. proxy-rotation's local HEAD
  `58b61f6` is unpushed; the pin is `origin/proxyrotation-dev` `2746784`.

## 2. What belongs in each row

- **Base**: upstream name and link.
- **Pin**: tag/commit/version and date.
- **License**: verified SPDX license, or “owner-declared; pending” if no
  LICENSE is committed.
- **What we use**: the smallest honest description.
- **Mode**: fork/merge, port, reference, runtime dependency, or data.
- **Review**: the cadence and trigger.

Do not add a repo to this table until we have decided how we use it and checked
its license.

## 3. Review and update procedure

### opencode2api (fork)

Use `plan/11`:

1. `upstream-drift.yml` reports new commits weekly.
2. Create `sync/upstream-YYYY-MM-DD` from `dev`; merge upstream.
3. Resolve conflicts with the patch rules; never accept “ours” wholesale for an
   upstream-owned file.
4. Run parity fixtures, identity hashes, `noext`, race tests, gitleaks/path scan.
5. Update `UPSTREAM_BASE`, `PATCHES.md`, and `CHANGELOG.md` in the same PR.
6. Merge to `dev`; promote only after Guard main + CI are green.

### Reference/port repositories

For proxy-rotation, 9Router, RTK, and OmniRoute:

1. Fetch the new tag/commit and read its changelog/diff since the pin.
2. Decide: adopt, port, or skip. Record the reason in the PR.
3. Port only the needed behavior into a new package or seam.
4. Add/update fixtures; do not copy code without a compatible license.
5. Update the pin, `NOTICE`/`PATCHES.md` if attribution changed, and the
   **Last reviewed** date in this file.
6. Do not add the reference repo as a runtime dependency or submodule.

### Headroom

1. Check the release notes since `0.42.0`.
2. Run the `/v1/compress` contract smoke against the new version.
3. Update the pin only if the smoke passes; otherwise show an upgrade warning.
4. Never auto-upgrade an existing user install.

### Metadata and WARP

- `models.dev` and OpenCode metadata refresh on a timer with schema validation
  and last-good fallback; no code merge.
- WARP has no repo to merge. Test registration, public egress key, quota
  behavior, and `/64` dedupe on a canary; treat changes as upstream behavior
  changes, not code updates.

## 4. Cadence

| When | What |
|---|---|
| Weekly | opencode2api drift issue; triage upstream PRs/issues |
| Monthly | review proxy-rotation, 9Router, RTK, OmniRoute, OpenCode, Headroom |
| Every release | opencode2api merge drill; update release notes with upstream base |
| Daily | models.dev/OpenCode metadata refresh; WARP/egress canary |
| Immediately | security-critical upstream change or free-lane breakage |

## 5. Known gaps and debts

- **proxy-rotation**: the MIT `LICENSE` is present in the owner's worktree
  (2026-10-28) but not yet pushed to `origin/proxyrotation-dev`. Verify it is
  pushed and confirm contributor rights, then add the MIT notice to `NOTICE`,
  before copying code or fixtures.
- **opencode2api**: use tag `v1.4.5` / `fe49a02`, not the dirty local checkout.
- **OmniRoute**: package version `3.8.53` at the pinned commit; nearest tag is
  `v3.8.52`, so the commit is the pin.
- **OpenCode**: pinned to a `dev` snapshot with package `2.1.4`; re-pin when
  provider/config behavior changes.
- **Headroom**: PyPI metadata does not show a license; the GitHub repository is
  Apache-2.0. Use the repository license for attribution.
- **Headroom local install**: the 9Router service on this laptop is `0.38.0`,
  not the pinned `0.42.0`. The `POST /v1/compress` core shape was observed on
  `0.38.0`; see `docs/headroom-smoke.md`. Owner decision (2026-10-28): keep the
  `0.42.0` pin, do not install or upgrade, and keep the pinned-version gate
  open. Do not restart the existing service.
- **models.dev / OpenCode metadata**: data, not code; no commit pin. Keep the
  last good copy and validate schemas.

## 6. What not to do

- Do not pin or copy a dirty working tree.
- Do not copy unlicensed code or fixtures.
- Do not auto-merge reference repositories.
- Do not add a Node/Python runtime just to reuse a reference implementation.
- Do not update `UPSTREAM_BASE` outside an upstream-sync PR.
- Do not silently change identity/TLS/WARP files; update goldens only with the
  `identity-update` review.
- Do not list a repo here without a review decision and a license decision
  (a pending license is allowed only with an explicit Phase 0 gate).
