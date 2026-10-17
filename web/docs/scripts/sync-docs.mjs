// SPDX-License-Identifier: Apache-2.0
//
// Publishes the repository's own Markdown (docs/, ADRs, architecture, plugin
// READMEs, TESTING.md, …) into the docs site's content tree. The originals
// stay the single source of truth; the generated copies are gitignored and
// rebuilt by `make docs` / `make docs-build`. Intra-repo links between synced
// files are rewritten to site routes; any other relative link (code paths,
// files not published here) is rendered as plain code so nothing 404s.
import { mkdirSync, readFileSync, rmSync, writeFileSync, readdirSync } from "node:fs";
import { dirname, join, posix, relative, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const here = dirname(fileURLToPath(import.meta.url));
const repo = resolve(here, "../../..");
const out = resolve(here, "../content");

/** [source path in repo, published route (no leading slash), sidebar title?] */
const pages = [
  ["sarvex-v1-architecture.md", "developers/architecture/overview", "Architecture"],
  ["sarvex-v1-sequence.md", "developers/architecture/sequences", "Cross-plane sequences"],
  ["TESTING.md", "developers/testing", "Testing"],
  ["SYNC_BACKGROUND_JOBS.md", "developers/architecture/sync-background-jobs", "Sync background jobs"],
  ["plugin/woocommerce/README.md", "developers/plugins/woocommerce", "WooCommerce plugin"],
  ["plugin/magento/README.md", "developers/plugins/magento", "Magento module"],
  ["docs/HOWTO-add-spec-version.md", "developers/extending/add-a-spec-version", "Add a spec version"],
  ["docs/HOWTO-add-platform.md", "developers/extending/add-a-platform", "Add a platform"],
  ["docs/HOWTO-add-surface.md", "developers/extending/add-a-surface", "Add an agent surface"],
  ["docs/IDEMPOTENCY-DESIGN.md", "developers/operations/idempotency-design", "Idempotency design"],
  ["docs/IDEMPOTENCY-RUNBOOK.md", "developers/operations/idempotency-runbook", "Idempotency runbook"],
  ["docs/SLO-ALERTING.md", "developers/operations/slo-alerting", "SLO alerting"],
  ["docs/TENANCY-PATTERNS.md", "contributing/tenancy-patterns", "Tenancy patterns"],
  ["docs/SECURITY-TEST-PLAN.md", "contributing/security-test-plan", "Security test plan"],
  ["docs/REAL-STORE-TEST-PLAN.md", "contributing/real-store-test-plan", "Real-store test plan"],
  ["CONTRIBUTING.md", "contributing/guide", "Contributing guide"],
  ["GOVERNANCE.md", "contributing/governance", "Governance"],
  ["SECURITY.md", "contributing/security-policy", "Security policy"],
  ["CODE_OF_CONDUCT.md", "contributing/code-of-conduct", "Code of conduct"],
];
for (const f of readdirSync(join(repo, "docs/adr")).filter((n) => /^\d{4}-.*\.md$/.test(n))) {
  pages.push([`docs/adr/${f}`, `contributing/adr/${f.replace(/\.md$/, "")}`]);
}

const routeOf = new Map(pages.map(([src, route]) => [src, route]));

// Previously generated pages are removed first so deleted sources don't linger.
// Only the pages go: hand-written meta.ts files beside them stay.
for (const [, route] of pages) rmSync(join(out, `${route}.md`), { force: true });
for (const dir of ["developers/architecture", "developers/plugins", "developers/extending",
  "developers/operations", "contributing/adr"]) {
  for (const f of readdirSync(join(out, dir)).filter((n) => n.endsWith(".md"))) {
    rmSync(join(out, dir, f), { force: true });
  }
}

// One-sentence summary for <meta>: cut at a word boundary, never mid-word.
const clip = (t) => (t.length <= 200 ? t : `${t.slice(0, 199).replace(/\s+\S*$/, "")}…`);
const yaml = (s) => JSON.stringify(s);

for (const [src, route, titleOverride] of pages) {
  let body = readFileSync(join(repo, src), "utf8");
  const h1 = body.match(/^#\s+(.+)$/m);
  const title = titleOverride ?? (h1 ? h1[1].replace(/[`*]/g, "").trim() : route.split("/").pop());
  if (h1) body = body.replace(h1[0], "").replace(/^\s*\n/, ""); // the page title renders from frontmatter
  const rawDescription = (body.split(/\n\s*\n/).map((p) => p.replace(/\s+/g, " ").trim())
    .find((p) => p && !/^[#>|`\-*\d<]/.test(p)) ?? title).replace(/\[([^\]]+)\]\([^)]*\)/g, "$1")
    .replace(/[`*]/g, "");

  const description = clip(rawDescription);
  const dir = posix.dirname(src);
  // Rewrite [text](relative/path[#frag]) — never touch absolute URLs or anchors.
  body = body.replace(/(?<!!)\[([^\]]+)\]\((?!https?:|mailto:|#|\/)([^)\s]+?)(#[^)\s]*)?\)/g,
    (_m, text, target, frag = "") => {
      const resolved = posix.normalize(posix.join(dir, target));
      const dest = routeOf.get(resolved);
      return dest ? `[${text}](/${dest}${frag})` : text === target ? `\`${target}\`` : `${text}`;
    });
  // Bare `file.md` style references in prose stay as written; they are code.

  const target = join(out, `${route}.md`);
  mkdirSync(dirname(target), { recursive: true });
  writeFileSync(target,
    `---\ntitle: ${yaml(title)}\ndescription: ${yaml(description)}\n---\n\n` +
    `*Generated from \`${src}\` in the repository — edit the source, not this page.*\n\n${body}`);
}
console.log(`sync-docs: published ${pages.length} pages from ${relative(process.cwd(), repo) || "."}`);
