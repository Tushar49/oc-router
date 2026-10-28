#!/usr/bin/env node
// Build the local binary and the pinned upstream binary, then run the fixture
// comparison in internal/parity. No network is required: the compared gateways
// talk to a local mock upstream.
//
// Usage:
//   node scripts/parity.mjs
//   PARITY_TMPDIR=/path node scripts/parity.mjs

import { execFileSync } from 'node:child_process';
import { existsSync, mkdtempSync, readFileSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { dirname, join, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';

const ROOT = dirname(dirname(fileURLToPath(import.meta.url)));
const UPSTREAM_FILE = join(ROOT, 'UPSTREAM_BASE');
const FIXTURES = join(ROOT, 'tests', 'fixtures');

function fail(message) {
  console.error(`parity: ${message}`);
  process.exit(1);
}

function readUpstreamCommit() {
  if (!existsSync(UPSTREAM_FILE)) fail(`${UPSTREAM_FILE} is missing`);
  const values = new Map();
  for (const line of readFileSync(UPSTREAM_FILE, 'utf8').split(/\r?\n/)) {
    const index = line.indexOf('=');
    if (index > 0) values.set(line.slice(0, index).trim(), line.slice(index + 1).trim());
  }
  const commit = values.get('commit');
  if (!commit) fail('UPSTREAM_BASE has no commit=');
  return commit;
}

function run(command, args, options = {}) {
  const label = [command, ...args].join(' ');
  console.log(`parity: ${label}`);
  try {
    execFileSync(command, args, { stdio: 'inherit', ...options });
  } catch (error) {
    if (options.allowFail) return false;
    throw error;
  }
  return true;
}

function main() {
  const commit = readUpstreamCommit();
  if (!existsSync(FIXTURES)) fail(`${FIXTURES} is missing`);
  const tempRoot = process.env.PARITY_TMPDIR || tmpdir();
  const work = mkdtempSync(join(tempRoot, 'oc-router-parity-'));
  const worktree = join(work, 'upstream');
  const extension = process.platform === 'win32' ? '.exe' : '';
  const ourBinary = join(work, `oc-router-our${extension}`);
  const upstreamBinary = join(work, `oc-router-upstream${extension}`);
  let added = false;
  try {
    if (!run('git', ['cat-file', '-e', `${commit}^{commit}`], { cwd: ROOT, allowFail: true })) {
      fail(`upstream commit ${commit} is not available; fetch the pinned upstream first`);
    }
    run('git', ['worktree', 'add', '--detach', '--force', worktree, commit], { cwd: ROOT });
    added = true;
    run('go', ['build', '-trimpath', '-o', ourBinary, './cmd/opencode2api'], { cwd: ROOT });
    run('go', ['build', '-trimpath', '-o', upstreamBinary, './cmd/opencode2api'], { cwd: worktree });
    run('go', ['test', './internal/parity', '-run', 'TestBinaries', '-count=1', '-v'], {
      cwd: ROOT,
      env: {
        ...process.env,
        PARITY_OUR_BIN: ourBinary,
        PARITY_UPSTREAM_BIN: upstreamBinary,
        PARITY_FIXTURES: FIXTURES,
      },
    });
  } finally {
    if (added) run('git', ['worktree', 'remove', '--force', worktree], { cwd: ROOT, allowFail: true });
    rmSync(work, { recursive: true, force: true });
  }
}

main();
