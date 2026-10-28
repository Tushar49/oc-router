# Development

The repository is a GPL-3.0-or-later fork of `opencode2api`, at upstream
baseline `v1.4.5` during Phase 0. Phase 0 is a scaffold: the commands below
build and test the upstream code. The identity hash manifest is present; the
fixture/parity runner is planned and not yet present.

## Requirements

- Go, matching `go.mod`.
- Node 22+ for the WebUI syntax check and identity/parity scripts (development
  tooling only; not a runtime dependency, and no npm install is needed).
- Optional: `staticcheck`, `govulncheck`, `gitleaks`.
- A CGO-capable toolchain (gcc) on Linux for `-race`.
- Do not install packages (npm/uv/pip) on the development machine. Use the
  tools already present; CI installs its own pinned tools in an ephemeral
  runner.

## Build and run

```sh
go build -o bin/oc-router ./cmd/opencode2api
go run ./cmd/opencode2api -config ./config.json
```

`bin/` and local config files are gitignored. Use a local `config.json`; never
commit it. Paths in the configuration should be relative or resolved from the
config/data directory, never from the process working directory.

## Formatting and static checks

```sh
gofmt -w cmd internal
gofmt -l .
go vet ./...
staticcheck ./...
govulncheck ./...
```

`gofmt -l .` must print nothing. `staticcheck` and `govulncheck` are optional
locally but are expected before a release. CI runs `go vet`, `go test`,
`gofmt`, `staticcheck`, `govulncheck`, the WebUI syntax check, the container
entrypoint syntax check, gitleaks, the path/secret scan, and the release build;
see `.github/workflows/ci.yml`. Identity checks and the noext gate live in
`.github/workflows/parity.yml`.

## Tests

```sh
go test ./...
```

With a CGO-capable toolchain (Linux CI):

```sh
go test ./... -race
```

Extensions-off parity build:

```sh
go test -tags noext ./...
```

The `noext` build registers no extension implementations and must preserve
upstream behavior. Add a test or fixture for every behavior change; if that is
not possible yet, document why in the PR.

## WebUI syntax

Bash / Linux / macOS:

```sh
for f in webui/js/*.js webui/js/pages/*.js; do
  node --input-type=module --check < "$f" || exit 1
done
```

PowerShell / Windows:

```powershell
Get-ChildItem webui/js/*.js, webui/js/pages/*.js | ForEach-Object {
  Get-Content -Raw $_.FullName | node --input-type=module --check
  if ($LASTEXITCODE -ne 0) { throw "syntax error: $($_.Name)" }
}
```

Also check the container entrypoint where applicable:

```sh
sh -n docker-entrypoint.sh
```

## Fixtures and parity

The identity hash manifest is implemented. The Phase 1 fixture/parity harness
is not. When it lands it will provide:

- golden fixtures for Chat, Responses, and Anthropic requests, non-stream
  responses, SSE sequences, error envelopes, and identity shaping;
- a mock upstream so tests make no real network calls;
- a parity runner that builds the pinned upstream binary and diffs normalized
  request/response artifacts.

The identity manifest is checked with:

```sh
node scripts/identity-hash.mjs --check
```

The intended parity entry points, to be confirmed when the packages exist:

```sh
go test ./internal/parity/...              # fixtures + parity
go test -tags noext ./internal/parity/...  # extensions-off parity
```

Do not claim these commands work until the harness is merged. A changed
identity hash will require an `identity-update` review and updated goldens.

## Upstream sync

See `docs/upstream-sync.md`. In short: branch `sync/upstream-YYYY-MM-DD` from
`dev`, merge `upstream/main` with `--no-ff`, re-apply small marked patches,
run the checks above, and update `UPSTREAM_BASE`, `PATCHES.md`, and
`CHANGELOG.md`.

## Platform notes

### Windows

- Use PowerShell for the WebUI loop above; `gofmt -l .` works the same.
- `-race` usually needs a CGO toolchain that is not present by default; run
  `go test ./...` and rely on Linux CI for the race job.
- Avoid hardcoded drive letters or user paths in code, docs, tests, and
  fixtures.
- Watch for CRLF churn: `.gitattributes` controls line endings; do not
  reformat unrelated files.

### Linux / macOS

- `-race` requires `CGO_ENABLED=1` and a C compiler.
- Case-sensitive filesystems can expose import/path mismatches that Windows
  hides; keep filenames and imports exact.

### Both

- Prefer `filepath` over string concatenation for paths.
- Resolve runtime paths from the config/data directory, not the current
  working directory.
- Keep logs and errors free of credentials, prompts, and absolute paths.

## Pull requests

Target `dev`. Run the checks above, keep the diff focused, and update docs and
`CHANGELOG.md` for user-visible changes. Security issues go through
`SECURITY.md`, not a public issue.
