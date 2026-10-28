#!/usr/bin/env node

import { execFileSync, spawnSync } from "node:child_process";
import { existsSync, readFileSync, writeFileSync } from "node:fs";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const repoRoot = resolve(dirname(fileURLToPath(import.meta.url)), "..");
const STRIP_PATHS = [
  "_archive",
  ".agents",
  ".research",
  ".githooks",
  ".main.gitignore",
  ".github/AGENTS.md",
  ".github/copilot-instructions.md",
];

process.chdir(repoRoot);

function fail(message) {
  throw new Error(message);
}

function git(args, options = {}) {
  const { buffer = false, stdio, ...rest } = options;
  return execFileSync("git", args, {
    cwd: repoRoot,
    encoding: buffer ? null : "utf8",
    stdio: stdio ?? ["ignore", "pipe", "pipe"],
    ...rest,
  });
}

function tryGit(args, options = {}) {
  const { buffer = false, stdio, ...rest } = options;
  return spawnSync("git", args, {
    cwd: repoRoot,
    encoding: buffer ? null : "utf8",
    stdio,
    ...rest,
  });
}

function text(args) {
  return String(git(args)).trim();
}

function textOrEmpty(args) {
  const result = tryGit(args);
  if (result.status !== 0) {
    return "";
  }
  return String(result.stdout ?? "").trim();
}

function isStripPath(path) {
  return STRIP_PATHS.some((entry) => path === entry || path.startsWith(`${entry}/`));
}

function currentBranch() {
  const result = tryGit(["symbolic-ref", "--quiet", "--short", "HEAD"]);
  return result.status === 0 ? String(result.stdout).trim() : "HEAD";
}

function abortMerge() {
  if (tryGit(["rev-parse", "-q", "--verify", "MERGE_HEAD"]).status === 0) {
    tryGit(["merge", "--abort"], { stdio: "inherit" });
  }
}

function usage() {
  console.log(`Usage: node scripts/promote.mjs <version> [options]

Merge dev into main, sync .main.gitignore, strip dev-only paths, verify, commit,
and push main. Never tags.

Options:
  --version <version>   Version label for the merge commit (positional also works)
  --remote <name>       Git remote to use (default: origin)
  --dry-run             Run merge, strip, and verification; do not commit or push
  -h, --help            Show this help
`);
}

function parseArgs(argv) {
  let version = "";
  let remote = "origin";
  let dryRun = false;

  for (let index = 0; index < argv.length; index += 1) {
    const arg = argv[index];
    if (arg === "--dry-run") {
      dryRun = true;
    } else if (arg === "--remote") {
      remote = argv[index + 1] ?? "";
      index += 1;
      if (!remote) {
        fail("--remote requires a remote name");
      }
    } else if (arg === "--version") {
      version = argv[index + 1] ?? "";
      index += 1;
      if (!version) {
        fail("--version requires a value");
      }
    } else if (arg === "--help" || arg === "-h") {
      usage();
      process.exit(0);
    } else if (arg.startsWith("-")) {
      fail(`unknown option: ${arg}`);
    } else if (!version) {
      version = arg;
    } else {
      fail(`unexpected argument: ${arg}`);
    }
  }

  if (!version) {
    try {
      version = readFileSync(join(repoRoot, "VERSION"), "utf8").trim().replace(/-dev$/, "");
    } catch {
      version = "";
    }
  }

  if (!version) {
    fail("a version is required: node scripts/promote.mjs <version>");
  }
  if (!/^[0-9A-Za-z._+-]+$/.test(version)) {
    fail(`invalid version label: ${version}`);
  }

  return { version, remote, dryRun };
}

function main() {
  const { version, remote, dryRun } = parseArgs(process.argv.slice(2));

  if (tryGit(["rev-parse", "--is-inside-work-tree"]).status !== 0) {
    fail("not inside a git working tree");
  }

  const originalBranch = currentBranch();
  if (originalBranch === "HEAD") {
    fail("detached HEAD; check out dev or another branch before promoting");
  }

  const dirty = textOrEmpty(["status", "--porcelain"]);
  if (dirty) {
    fail(`working tree is not clean; commit or stash changes first:\n${dirty}`);
  }

  const remoteMain = `${remote}/main`;
  const remoteDev = `${remote}/dev`;

  try {
    console.log(`promote: fetching ${remote}`);
    git(["fetch", remote, "--tags", "--prune"], { stdio: "inherit" });

    for (const ref of [remoteMain, remoteDev]) {
      if (tryGit(["rev-parse", "--verify", ref]).status !== 0) {
        fail(`missing remote ref: ${ref}`);
      }
    }

    const hasDevMainIgnore = tryGit(["cat-file", "-e", `${remoteDev}:.main.gitignore`]).status === 0;

    console.log(`promote: checking out main and fast-forwarding from ${remote}`);
    if (tryGit(["show-ref", "--verify", "--quiet", "refs/heads/main"]).status === 0) {
      git(["checkout", "main"], { stdio: "inherit" });
    } else {
      git(["checkout", "-b", "main", remoteMain], { stdio: "inherit" });
    }
    git(["pull", "--ff-only", remote, "main"], { stdio: "inherit" });

    console.log(`promote: merging ${remoteDev} with --no-ff --no-commit`);
    const merge = tryGit(["merge", "--no-ff", "--no-commit", remoteDev], { stdio: "inherit" });
    let unmerged = [];
    if (merge.status !== 0) {
      unmerged = textOrEmpty(["diff", "--name-only", "--diff-filter=U"])
        .split("\n")
        .filter(Boolean);

      if (unmerged.length === 0) {
        const detail = String(merge.stderr ?? "").trim();
        fail(`git merge failed${detail ? `: ${detail}` : ""}`);
      }

      const outside = unmerged.filter((path) => !isStripPath(path) && path !== ".gitignore");
      if (outside.length > 0) {
        fail(`merge conflicts outside the strip list:\n${outside.join("\n")}`);
      }
      if (unmerged.includes(".gitignore") && !hasDevMainIgnore) {
        fail("the .gitignore conflict cannot be resolved because dev has no .main.gitignore");
      }

      for (const path of unmerged) {
        if (path === ".gitignore") {
          continue;
        }
        tryGit(["rm", "-rf", "-q", "--ignore-unmatch", "--", path], { stdio: "inherit" });
      }
    }

    if (hasDevMainIgnore) {
      const desired = git(["show", `${remoteDev}:.main.gitignore`], { buffer: true });
      const current = existsSync(join(repoRoot, ".gitignore"))
        ? readFileSync(join(repoRoot, ".gitignore"))
        : Buffer.alloc(0);
      if (!desired.equals(current)) {
        writeFileSync(join(repoRoot, ".gitignore"), desired);
        git(["add", ".gitignore"]);
        console.log("promote: synced .gitignore from .main.gitignore");
      } else {
        console.log("promote: .gitignore already matches .main.gitignore");
      }
      if (textOrEmpty(["ls-files", "-u", "--", ".gitignore"])) {
        git(["add", ".gitignore"]);
      }
    }

    console.log("promote: stripping dev-only paths");
    git(["rm", "-rf", "-q", "--ignore-unmatch", "--", ...STRIP_PATHS], { stdio: "inherit" });

    const tracked = textOrEmpty(["ls-files", "--", ...STRIP_PATHS]);
    if (tracked) {
      fail(`a dev-only path is still tracked:\n${tracked}`);
    }

    const unmergedNow = textOrEmpty(["ls-files", "-u"]);
    if (unmergedNow) {
      fail(`unmerged paths remain:\n${unmergedNow}`);
    }

    const unstaged = textOrEmpty(["diff", "--name-only"]);
    if (unstaged) {
      fail(`unstaged changes remain:\n${unstaged}`);
    }

    const untracked = textOrEmpty(["ls-files", "--others", "--exclude-standard"]);
    if (untracked) {
      fail(`untracked files remain:\n${untracked}`);
    }

    const hasStaged = tryGit(["diff", "--cached", "--quiet"]).status !== 0;
    if (!hasStaged) {
      console.log("promote: dev is already merged into main; nothing to promote");
      return;
    }

    if (dryRun) {
      git(["diff", "--cached", "--stat"], { stdio: "inherit" });
      console.log("promote: dry run passed; no commit and no push");
      return;
    }

    console.log(`promote: committing Merge dev -> main: ${version}`);
    git(["commit", "-m", `Merge dev -> main: ${version}`], { stdio: "inherit" });

    const committedDevOnly = textOrEmpty(["ls-tree", "-r", "--name-only", "HEAD", "--", ...STRIP_PATHS]);
    if (committedDevOnly) {
      fail(`pre-push guard failed; a dev-only path is in the commit:\n${committedDevOnly}`);
    }

    const postCommitStatus = textOrEmpty(["status", "--porcelain"]);
    if (postCommitStatus) {
      fail(`pre-push guard failed; working tree is not clean:\n${postCommitStatus}`);
    }

    console.log(`promote: pushing main to ${remote}`);
    git(["push", remote, "main"], { stdio: "inherit" });
    console.log("promote: pushed main; no tag was created");
  } finally {
    abortMerge();
    const branch = currentBranch();
    if (branch !== "HEAD" && branch !== originalBranch) {
      tryGit(["checkout", originalBranch], { stdio: "inherit" });
    }
  }
}

try {
  main();
} catch (error) {
  console.error(`promote: ${error.message}`);
  process.exitCode = 1;
}
