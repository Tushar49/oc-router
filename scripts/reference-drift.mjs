#!/usr/bin/env node

import { readFileSync, writeFileSync } from "node:fs";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const repoRoot = resolve(dirname(fileURLToPath(import.meta.url)), "..");
const referencePath = join(repoRoot, "REFERENCE_BASES.md");
const token = process.env.GITHUB_TOKEN || process.env.GH_TOKEN || "";

function fail(message) {
  throw new Error(message);
}

function parseArgs(argv) {
  let output = "";
  let jsonOutput = "";
  let failOnDrift = false;

  for (let index = 0; index < argv.length; index += 1) {
    const arg = argv[index];
    if (arg === "--output") {
      output = argv[index + 1] ?? "";
      index += 1;
      if (!output) {
        fail("--output requires a path");
      }
    } else if (arg === "--json") {
      jsonOutput = argv[index + 1] ?? "";
      index += 1;
      if (!jsonOutput) {
        fail("--json requires a path");
      }
    } else if (arg === "--fail-on-drift") {
      failOnDrift = true;
    } else if (arg === "--help" || arg === "-h") {
      console.log(`Usage: node scripts/reference-drift.mjs [options]

Read REFERENCE_BASES.md and report new tags, HEAD commits, and license changes
for pinned GitHub repositories. This script only reads; it never merges or writes
to a repository.

Options:
  --output <path>       Write the Markdown report to a file
  --json <path>         Write the raw results as JSON
  --fail-on-drift       Exit non-zero when drift or a license change is found
  -h, --help            Show this help
`);
      process.exit(0);
    } else {
      fail(`unknown option: ${arg}`);
    }
  }

  return { output, jsonOutput, failOnDrift };
}

function parseReferences(markdown) {
  const rows = [];
  let inSection = false;

  for (const raw of markdown.split(/\r?\n/)) {
    if (/^##\s+1\./.test(raw)) {
      inSection = true;
      continue;
    }
    if (inSection && /^##\s+/.test(raw)) {
      break;
    }
    if (!inSection || !raw.trim().startsWith("|")) {
      continue;
    }

    const cells = raw
      .split("|")
      .slice(1, -1)
      .map((cell) => cell.trim());
    if (cells.length < 6) {
      continue;
    }
    if (/^:?-{3,}/.test(cells[0])) {
      continue;
    }
    if (cells[0].toLowerCase() === "base") {
      continue;
    }

    const link = /^\[([^\]]+)\]\((https?:\/\/[^)\s]+)\)/.exec(cells[0]);
    const name = link ? link[1].trim() : cells[0].replace(/[*`]/g, "").trim();
    const bareUrl = (value) => (value.match(/https?:\/\/[^\s`;,)]+/) || [""])[0];
    const url = link ? link[2] : bareUrl(cells[0]) || bareUrl(cells[1]);

    const pinCell = cells[1];
    const commitMatch = pinCell.match(/\b([0-9a-f]{40})\b/i);
    const commit = commitMatch ? commitMatch[1].toLowerCase() : "";
    const date = (pinCell.match(/\b(\d{4}-\d{2}-\d{2})\b/) || ["", ""])[1];
    const refs = [...pinCell.matchAll(/`([^`]+)`/g)].map((match) => match[1]);
    const pinnedRef = refs.find((ref) => !/^[0-9a-f]{40}$/i.test(ref)) || "";

    const licenseRaw = cells[2].replace(/[*`]/g, "").trim();
    const licenseToken = (licenseRaw.match(/[A-Za-z0-9.+-]+/) || [""])[0];

    const github = /^https?:\/\/github\.com\/([^/]+)\/([^/#?\s]+)/i.exec(url);

    rows.push({
      name,
      url: url.replace(/[.,;]+$/, ""),
      pinnedRef,
      commit,
      date,
      licenseRaw,
      licenseToken,
      use: cells[3],
      mode: cells[4],
      review: cells[5],
      owner: github ? github[1] : "",
      repo: github ? github[2].replace(/\.git$/i, "") : "",
    });
  }

  return rows;
}

function apiHeaders() {
  const headers = {
    Accept: "application/vnd.github+json",
    "User-Agent": "oc-router-reference-drift",
    "X-GitHub-Api-Version": "2022-11-28",
  };
  if (token) {
    headers.Authorization = `Bearer ${token}`;
  }
  return headers;
}

async function api(path) {
  const response = await fetch(`https://api.github.com${path}`, {
    headers: apiHeaders(),
  });

  if (response.status === 404) {
    return { status: 404, data: null, remaining: response.headers.get("x-ratelimit-remaining") };
  }
  if (!response.ok) {
    const body = await response.text();
    throw new Error(`GitHub API ${path} returned ${response.status}: ${body.slice(0, 300)}`);
  }

  return {
    status: response.status,
    data: await response.json(),
    remaining: response.headers.get("x-ratelimit-remaining"),
  };
}

function normalizeSpdx(value) {
  return String(value ?? "")
    .toLowerCase()
    .replace(/[^a-z0-9.+-]/g, "");
}

function baseLicense(value) {
  return normalizeSpdx(value).replace(/-or-later$/, "").replace(/-only$/, "");
}

function licensesEquivalent(pinned, current) {
  if (!pinned || !current) {
    return true;
  }
  return baseLicense(pinned) === baseLicense(current);
}

function parseVersion(value) {
  const match = /^v?(\d+)\.(\d+)\.(\d+)(?:-([0-9A-Za-z.-]+))?$/.exec(String(value ?? ""));
  if (!match) {
    return null;
  }
  return {
    major: Number(match[1]),
    minor: Number(match[2]),
    patch: Number(match[3]),
    pre: match[4] ?? "",
  };
}

function compareVersions(left, right) {
  for (const key of ["major", "minor", "patch"]) {
    if (left[key] !== right[key]) {
      return left[key] - right[key];
    }
  }
  if (left.pre === right.pre) {
    return 0;
  }
  if (!left.pre) {
    return 1;
  }
  if (!right.pre) {
    return -1;
  }
  return left.pre.localeCompare(right.pre);
}

function firstLine(value) {
  return String(value ?? "").split(/\r?\n/)[0].trim();
}

async function checkGitHubRow(row) {
  const { owner, repo } = row;
  const base = `/repos/${owner}/${repo}`;
  const result = {
    row,
    kind: "github",
    repoFullName: `${owner}/${repo}`,
    defaultBranch: "",
    headSha: "",
    headDate: "",
    headMessage: "",
    aheadBy: null,
    behindBy: null,
    totalCommits: null,
    currentLicense: "",
    licenseChanged: false,
    newTags: [],
    tagsChecked: 0,
    pinnedRefFound: false,
    archived: false,
    renamedTo: "",
    error: "",
    notes: [],
    drift: false,
  };

  let repoResponse;
  try {
    repoResponse = await api(base);
  } catch (error) {
    result.error = error.message;
    return result;
  }

  if (repoResponse.status === 404) {
    result.error = "Repository not found (404); it may have been renamed, made private, or removed.";
    return result;
  }

  const repoData = repoResponse.data;
  result.defaultBranch = repoData.default_branch || row.pinnedRef || "main";
  result.currentLicense = repoData.license?.spdx_id || "";
  result.archived = Boolean(repoData.archived);
  if (repoData.full_name && repoData.full_name.toLowerCase() !== result.repoFullName.toLowerCase()) {
    result.renamedTo = repoData.full_name;
  }

  try {
    const headResponse = await api(
      `${base}/commits/${encodeURIComponent(result.defaultBranch)}`,
    );
    if (headResponse.status === 404) {
      result.notes.push(`default branch \`${result.defaultBranch}\` not found`);
    } else {
      result.headSha = headResponse.data.sha || "";
      result.headDate = headResponse.data.commit?.committer?.date || "";
      result.headMessage = firstLine(headResponse.data.commit?.message);
    }
  } catch (error) {
    result.notes.push(`HEAD lookup failed: ${error.message}`);
  }

  try {
    const tagsResponse = await api(`${base}/tags?per_page=100`);
    const tags = Array.isArray(tagsResponse.data) ? tagsResponse.data : [];
    result.tagsChecked = tags.length;
    result.pinnedRefFound = Boolean(
      row.pinnedRef && tags.some((tag) => tag.name === row.pinnedRef),
    );
    const pinnedVersion = parseVersion(row.pinnedRef);
    if (pinnedVersion) {
      result.newTags = tags
        .map((tag) => ({ name: tag.name, version: parseVersion(tag.name) }))
        .filter((tag) => tag.name !== row.pinnedRef)
        .filter((tag) => tag.version && compareVersions(tag.version, pinnedVersion) > 0)
        .sort((left, right) => compareVersions(right.version, left.version))
        .slice(0, 8)
        .map((tag) => tag.name);
    } else if (row.pinnedRef) {
      result.notes.push("pinned ref is a branch or non-semver tag; new tags are not ranked");
    }
    if (row.pinnedRef && !result.pinnedRefFound && tags.length === 100) {
      result.notes.push("pinned ref not in the first 100 tags (it may be a branch or an older tag)");
    }
  } catch (error) {
    result.notes.push(`tag lookup failed: ${error.message}`);
  }

  if (row.commit && result.headSha) {
    result.drift = result.headSha !== row.commit;
    try {
      const compareResponse = await api(
        `${base}/compare/${row.commit}...${encodeURIComponent(result.defaultBranch)}`,
      );
      if (compareResponse.status !== 404) {
        result.aheadBy = compareResponse.data.ahead_by ?? null;
        result.behindBy = compareResponse.data.behind_by ?? null;
        result.totalCommits = compareResponse.data.total_commits ?? null;
      }
    } catch (error) {
      result.notes.push(`compare failed: ${error.message}`);
    }
  }

  const pinnedSpdx = normalizeSpdx(row.licenseToken);
  const currentSpdx = normalizeSpdx(result.currentLicense);
  if (pinnedSpdx && currentSpdx && !licensesEquivalent(row.licenseToken, result.currentLicense)) {
    result.licenseChanged = true;
    result.drift = true;
  }
  if (!result.currentLicense) {
    result.notes.push("GitHub API reports no detected license");
  }
  if (/owner-declared|pending/i.test(row.licenseRaw)) {
    result.notes.push("pin license is owner-declared or pending verification");
  }
  if (result.archived) {
    result.notes.push("repository is archived");
  }
  if (result.renamedTo) {
    result.notes.push(`repository is now ${result.renamedTo}`);
  }

  return result;
}

function markdownCell(value) {
  return String(value ?? "")
    .replace(/\|/g, "\\|")
    .replace(/\r?\n/g, " ")
    .trim();
}

function buildReport(results) {
  const lines = [];
  const generated = new Date().toISOString();

  lines.push("# Reference drift report");
  lines.push("");
  lines.push(`Generated: ${generated}`);
  lines.push("Source: `REFERENCE_BASES.md` section 1 (Pinned bases)");
  lines.push("");
  lines.push("| Base | Pinned | HEAD | Drift | License | Notes |");
  lines.push("|---|---|---|---|---|---|");

  for (const result of results) {
    const { row } = result;
    const pinned = [row.pinnedRef, row.commit ? row.commit.slice(0, 12) : ""]
      .filter(Boolean)
      .join(" @ ");

    if (result.kind !== "github") {
      lines.push(
        `| ${markdownCell(row.name)} | ${markdownCell(pinned || row.licenseRaw)} | n/a | n/a | ${markdownCell(row.licenseRaw)} | Not a GitHub repository; review the pin manually. |`,
      );
      continue;
    }

    if (result.error) {
      lines.push(
        `| ${markdownCell(row.name)} | ${markdownCell(pinned)} | unknown | unknown | ${markdownCell(result.currentLicense || row.licenseRaw)} | ${markdownCell(result.error)} |`,
      );
      continue;
    }

    const head = result.headSha
      ? `${result.defaultBranch}@${result.headSha.slice(0, 12)}`
      : "unknown";
    let drift = "none";
    if (result.drift) {
      const parts = [];
      if (result.aheadBy !== null) {
        parts.push(`ahead ${result.aheadBy}`);
      }
      if (result.behindBy !== null) {
        parts.push(`behind ${result.behindBy}`);
      }
      drift = parts.length > 0 ? parts.join(", ") : "changed";
    }
    if (result.licenseChanged) {
      drift = drift === "none" ? "license changed" : `${drift}; license changed`;
    }

    const notes = [...result.notes];
    if (result.newTags.length > 0) {
      notes.push(`${result.newTags.length} new tag(s) shown`);
    }

    lines.push(
      `| ${markdownCell(row.name)} | ${markdownCell(pinned)} | ${markdownCell(head)} | ${markdownCell(drift)} | ${markdownCell(result.currentLicense || "none detected")} | ${markdownCell(notes.join("; ") || "current")} |`,
    );
  }

  lines.push("");
  lines.push("## Details");
  lines.push("");

  for (const result of results) {
    const { row } = result;
    lines.push(`### ${row.name}`);
    lines.push("");
    lines.push(`- URL: ${row.url || "n/a"}`);
    lines.push(`- Mode: ${row.mode || "n/a"}`);
    lines.push(`- What we use: ${row.use || "n/a"}`);
    lines.push(`- Review cadence: ${row.review || "n/a"}`);
    lines.push(`- Pinned ref: ${row.pinnedRef || "n/a"}`);
    lines.push(`- Pinned commit: ${row.commit || "n/a"}`);
    lines.push(`- Pinned date: ${row.date || "n/a"}`);
    lines.push(`- Pinned license: ${row.licenseRaw || "n/a"}`);

    if (result.kind !== "github") {
      lines.push("- Not a GitHub repository; no API check was performed.");
      lines.push("");
      continue;
    }

    if (result.error) {
      lines.push(`- Error: ${result.error}`);
      lines.push("");
      continue;
    }

    lines.push(`- Default branch: ${result.defaultBranch}`);
    lines.push(
      `- HEAD: ${result.headSha || "unknown"}${result.headDate ? ` (${result.headDate})` : ""}`,
    );
    if (result.headMessage) {
      lines.push(`- HEAD message: ${result.headMessage}`);
    }
    if (result.aheadBy !== null) {
      lines.push(`- Commits ahead of the pin: ${result.aheadBy}`);
    }
    if (result.behindBy !== null) {
      lines.push(`- Commits behind the pin: ${result.behindBy}`);
    }
    if (result.totalCommits !== null) {
      lines.push(`- Commits in compare range: ${result.totalCommits}`);
    }
    lines.push(`- Current license: ${result.currentLicense || "none detected"}`);
    lines.push(`- License changed: ${result.licenseChanged ? "yes" : "no"}`);
    lines.push(
      `- New tags: ${result.newTags.length > 0 ? result.newTags.join(", ") : "none in the first 100 tags"}`,
    );
    if (result.notes.length > 0) {
      lines.push(`- Notes: ${result.notes.join("; ")}`);
    }
    lines.push("");
  }

  lines.push("This report is read-only. Update pins only through a reviewed PR.");
  lines.push("");

  return lines.join("\n");
}

async function main() {
  const { output, jsonOutput, failOnDrift } = parseArgs(process.argv.slice(2));

  const markdown = readFileSync(referencePath, "utf8");
  const rows = parseReferences(markdown);
  if (rows.length === 0) {
    fail("no pinned bases found in REFERENCE_BASES.md section 1");
  }

  const results = [];
  for (const row of rows) {
    if (row.owner && row.repo) {
      results.push(await checkGitHubRow(row));
    } else {
      results.push({
        row,
        kind: "non-github",
        error: "",
        drift: false,
        licenseChanged: false,
        newTags: [],
        notes: [],
      });
    }
  }

  const report = buildReport(results);
  process.stdout.write(report);

  if (output) {
    writeFileSync(resolve(process.cwd(), output), report);
  }
  if (jsonOutput) {
    writeFileSync(
      resolve(process.cwd(), jsonOutput),
      `${JSON.stringify(
        {
          generated: new Date().toISOString(),
          source: "REFERENCE_BASES.md",
          results,
        },
        null,
        2,
      )}\n`,
    );
  }

  const drifted = results.some(
    (result) => result.drift || result.licenseChanged || result.error,
  );
  if (failOnDrift && drifted) {
    process.exitCode = 1;
  }
}

main().catch((error) => {
  console.error(`reference-drift: ${error.message}`);
  process.exitCode = 1;
});
