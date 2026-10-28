#!/usr/bin/env node
// Identity hash manifest for read-only free-lane identity files.
//
// Hashing these bytes makes accidental "cleanup" fail loudly. The manifest is
// intentionally dependency-free and path-stable so it can run on every OS in
// CI without a Go toolchain.
//
// Usage:
//   node scripts/identity-hash.mjs           # write docs/identity-manifest.json
//   node scripts/identity-hash.mjs --check   # compare tree to the manifest
//   node scripts/identity-hash.mjs --help
//
// Exit codes: 0 = ok, 1 = drift/missing manifest, 2 = usage error.

import { createHash } from 'node:crypto';
import {
  existsSync,
  mkdirSync,
  readFileSync,
  readdirSync,
  statSync,
  writeFileSync,
} from 'node:fs';
import { dirname, join, relative, sep } from 'node:path';
import { fileURLToPath } from 'node:url';

const SCHEMA_VERSION = 1;
const ALGORITHM = 'sha256';
const MANIFEST_PATH = 'docs/identity-manifest.json';

const ROOT = dirname(dirname(fileURLToPath(import.meta.url)));

const EXPLICIT_FILES = [
  'internal/httpx/bun.go',
  'internal/httpx/bun_dial.go',
  'internal/gateway/opencode_prompt.go',
  'internal/gateway/toolshape.go',
];

const GLOB_DIRS = [
  { dir: 'internal/identity', match: (name) => name.endsWith('.go') },
  { dir: 'internal/warp', match: (name) => name.endsWith('.go') },
];

const EMBED_RE = /^\s*\/\/go:embed\s+(.+?)\s*$/;
const META_RE = /[*?]/;

function toPosix(value) {
  return value.split(sep).join('/');
}

function fromRoot(root, absolutePath) {
  return toPosix(relative(root, absolutePath));
}

function isInsideRoot(root, absolutePath) {
  const rel = relative(root, absolutePath);
  return rel !== '' && !rel.startsWith('..') && !rel.includes(`..${sep}`);
}

function walk(dir, accept) {
  const files = [];
  if (!existsSync(dir)) return files;
  for (const entry of readdirSync(dir, { withFileTypes: true })) {
    const full = join(dir, entry.name);
    if (entry.isDirectory()) {
      files.push(...walk(full, accept));
    } else if (entry.isFile() && accept(entry.name)) {
      files.push(full);
    }
  }
  return files;
}

function splitEmbedTokens(raw) {
  const tokens = [];
  const re = /"([^"]*)"|`([^`]*)`|(\S+)/g;
  let match;
  while ((match = re.exec(raw)) !== null) {
    tokens.push(match[1] ?? match[2] ?? match[3]);
  }
  return tokens;
}

function patternToRegExp(pattern) {
  let source = '';
  for (const char of pattern) {
    if (char === '*') source += '[^/]*';
    else if (char === '?') source += '[^/]';
    else source += char.replace(/[.+^${}()|[\]\\]/g, '\\$&');
  }
  return new RegExp(`^${source}$`);
}

function collectEmbeddedFiles(goFile) {
  const goDir = dirname(goFile);
  const text = readFileSync(goFile, 'utf8');
  const found = [];

  for (const line of text.split(/\r?\n/)) {
    const match = EMBED_RE.exec(line);
    if (!match) continue;
    for (const token of splitEmbedTokens(match[1])) {
      found.push(...expandEmbedPattern(goDir, token));
    }
  }
  return found;
}

function expandEmbedPattern(goDir, pattern) {
  const normalized = pattern.replace(/\\/g, '/').replace(/^\.\//, '');
  if (normalized === '' || normalized.startsWith('/') || normalized.split('/').includes('..')) {
    return [];
  }

  if (!META_RE.test(normalized)) {
    const full = join(goDir, ...normalized.split('/'));
    if (!existsSync(full)) return [];
    const stat = statSync(full);
    if (stat.isDirectory()) return walk(full, () => true);
    return stat.isFile() ? [full] : [];
  }

  const parts = normalized.split('/');
  let firstMeta = 0;
  while (firstMeta < parts.length && !META_RE.test(parts[firstMeta])) firstMeta += 1;

  const baseDir = join(goDir, ...parts.slice(0, firstMeta));
  const baseParts = parts.slice(firstMeta);
  if (baseParts.length === 0 || !existsSync(baseDir)) return [];

  const re = patternToRegExp(baseParts.join('/'));
  const results = [];

  const scan = (dir, relPrefix) => {
    for (const entry of readdirSync(dir, { withFileTypes: true })) {
      if (entry.name.startsWith('.') || entry.name.startsWith('_')) continue;
      const rel = relPrefix === '' ? entry.name : `${relPrefix}/${entry.name}`;
      const full = join(dir, entry.name);
      if (entry.isDirectory()) {
        if (re.test(rel)) results.push(...walk(full, () => true));
        else scan(full, rel);
      } else if (entry.isFile() && re.test(rel)) {
        results.push(full);
      }
    }
  };

  scan(baseDir, '');
  return results;
}

function collectIdentityFiles(root) {
  const selected = new Map();

  const add = (absolutePath) => {
    if (!isInsideRoot(root, absolutePath)) return;
    if (!statSync(absolutePath).isFile()) return;
    const rel = fromRoot(root, absolutePath);
    if (!selected.has(rel)) selected.set(rel, absolutePath);
  };

  for (const rel of EXPLICIT_FILES) {
    const full = join(root, ...rel.split('/'));
    if (existsSync(full)) add(full);
  }

  for (const { dir, match } of GLOB_DIRS) {
    for (const full of walk(join(root, ...dir.split('/')), match)) add(full);
  }

  const goFiles = [...selected.values()].filter((file) => file.endsWith('.go'));
  for (const goFile of goFiles) {
    for (const embedded of collectEmbeddedFiles(goFile)) add(embedded);
  }

  return [...selected.entries()]
    .map(([rel, absolutePath]) => ({ path: rel, absolutePath }))
    .sort((a, b) => (a.path < b.path ? -1 : a.path > b.path ? 1 : 0));
}

function hashFile(absolutePath) {
  return createHash(ALGORITHM).update(readFileSync(absolutePath)).digest('hex');
}

function readUpstreamBase(root) {
  const file = join(root, 'UPSTREAM_BASE');
  if (!existsSync(file)) {
    return { repo: null, tag: null, commit: null, date: null };
  }
  const values = {};
  for (const line of readFileSync(file, 'utf8').split(/\r?\n/)) {
    const trimmed = line.trim();
    if (trimmed === '' || trimmed.startsWith('#')) continue;
    const match = /^([A-Za-z0-9_.-]+)\s*=\s*(.*)$/.exec(trimmed);
    if (match) values[match[1]] = match[2];
  }
  return {
    repo: values.repo ?? null,
    tag: values.tag ?? null,
    commit: values.commit ?? null,
    date: values.date ?? null,
  };
}

function buildManifest(root) {
  const files = collectIdentityFiles(root).map(({ path, absolutePath }) => ({
    path,
    [ALGORITHM]: hashFile(absolutePath),
  }));

  for (const entry of files) {
    if (entry.path.startsWith('/') || /^[A-Za-z]:/.test(entry.path) || entry.path.split('/').includes('..')) {
      throw new Error(`refusing to write non-relative path: ${entry.path}`);
    }
  }

  return {
    schemaVersion: SCHEMA_VERSION,
    algorithm: ALGORITHM,
    generated: new Date().toISOString().slice(0, 10),
    upstream: readUpstreamBase(root),
    files,
  };
}

function comparable(manifest) {
  return {
    schemaVersion: manifest?.schemaVersion,
    algorithm: manifest?.algorithm,
    upstream: manifest?.upstream ?? null,
    files: Array.isArray(manifest?.files) ? manifest.files : null,
  };
}

function describeUpstreamDiff(expected, actual) {
  const keys = ['repo', 'tag', 'commit', 'date'];
  const changed = keys.filter((key) => (expected?.[key] ?? null) !== (actual?.[key] ?? null));
  if (changed.length === 0) return [];
  return changed.map(
    (key) => `upstream.${key}: manifest=${JSON.stringify(expected?.[key] ?? null)} tree=${JSON.stringify(actual?.[key] ?? null)}`,
  );
}

function checkManifest(root, manifestPath) {
  if (!existsSync(manifestPath)) {
    console.error(`identity manifest missing: ${MANIFEST_PATH}`);
    console.error('run `node scripts/identity-hash.mjs` to create it');
    return 1;
  }

  let manifest;
  try {
    manifest = JSON.parse(readFileSync(manifestPath, 'utf8'));
  } catch (error) {
    console.error(`identity manifest is not valid JSON: ${error.message}`);
    return 1;
  }

  const current = buildManifest(root);
  const problems = [];

  if (manifest.schemaVersion !== SCHEMA_VERSION) {
    problems.push(`schemaVersion: manifest=${manifest.schemaVersion} expected=${SCHEMA_VERSION}`);
  }
  if (manifest.algorithm !== ALGORITHM) {
    problems.push(`algorithm: manifest=${manifest.algorithm} expected=${ALGORITHM}`);
  }
  problems.push(...describeUpstreamDiff(manifest.upstream, current.upstream));

  if (!Array.isArray(manifest.files)) {
    problems.push('files: manifest entry is missing or not an array');
  } else {
    const expected = new Map(manifest.files.map((entry) => [entry.path, entry[ALGORITHM]]));
    const actual = new Map(current.files.map((entry) => [entry.path, entry[ALGORITHM]]));

    for (const [path, hash] of expected) {
      if (!actual.has(path)) problems.push(`removed: ${path}`);
      else if (actual.get(path) !== hash) {
        problems.push(`changed: ${path} (manifest=${hash} tree=${actual.get(path)})`);
      }
    }
    for (const path of actual.keys()) {
      if (!expected.has(path)) problems.push(`added: ${path}`);
    }
  }

  if (problems.length > 0) {
    console.error('identity hash drift detected:');
    for (const problem of problems) console.error(`  - ${problem}`);
    console.error('');
    console.error('If this is intentional, open an `identity-update` PR with updated');
    console.error('goldens and the upstream reason, then run:');
    console.error('  node scripts/identity-hash.mjs');
    return 1;
  }

  console.log(`identity manifest ok (${current.files.length} files)`);
  return 0;
}

function writeManifest(root, manifestPath) {
  const manifest = buildManifest(root);
  mkdirSync(dirname(manifestPath), { recursive: true });
  writeFileSync(manifestPath, `${JSON.stringify(manifest, null, 2)}\n`, 'utf8');
  console.log(`wrote ${MANIFEST_PATH} (${manifest.files.length} files)`);
  return 0;
}

function usage() {
  console.log('usage: node scripts/identity-hash.mjs [--check]');
  console.log('');
  console.log('  (no args)  write docs/identity-manifest.json');
  console.log('  --check    compare the tree against docs/identity-manifest.json');
  console.log('  --help     show this help');
}

function main(argv) {
  const args = argv.slice(2);
  if (args.some((arg) => arg === '--help' || arg === '-h')) {
    usage();
    return 0;
  }

  const unknown = args.filter((arg) => arg !== '--check');
  if (unknown.length > 0) {
    console.error(`unknown argument: ${unknown.join(' ')}`);
    usage();
    return 2;
  }

  const manifestPath = join(ROOT, ...MANIFEST_PATH.split('/'));
  if (args.includes('--check')) return checkManifest(ROOT, manifestPath);
  return writeManifest(ROOT, manifestPath);
}

try {
  process.exitCode = main(process.argv);
} catch (error) {
  console.error(`identity-hash failed: ${error.message}`);
  process.exitCode = 1;
}
