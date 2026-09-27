// guard: crumb-link-truth (REVIEW-46 O76). PageHeaderSkeleton's parent crumb is a link by default (a 44px
// tap box below md); a page whose parent crumb has no href renders a 22px text crumb. The skeleton's
// `crumbLink` must follow the page's own `crumbs={[...]}`: this test reads both and fails on a mismatch,
// and every `crumbLink={false}` in the tree must be listed here (so a new one is checked too).
import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync, readdirSync, statSync } from "node:fs";
import { join, relative } from "node:path";
import { fileURLToPath } from "node:url";

const root = fileURLToPath(new URL("../../../", import.meta.url));
const read = (rel) => readFileSync(join(root, rel), "utf8");

/** skeleton file -> the page file(s) whose PageHeader it twins. */
const TWINS = {
  "app/(admin)/vaccination/loading.tsx": ["features/preventive-care-vaccination/operations.tsx"],
  "app/(admin)/vaccination/plan/loading.tsx": ["features/vaccination-plan/plan-console.tsx"],
  "app/(admin)/vaccination/care-coverage/loading.tsx": ["features/vaccination-care-coverage/care-coverage-board.tsx"],
  "app/(admin)/herd-signals/loading.tsx": ["features/herd-signals/herd-signals-board.tsx"],
  "app/(admin)/weighing/weights/loading.tsx": ["features/weighing/weights.tsx"],
  "app/(admin)/weighing/analytics/loading.tsx": ["features/weighing/weights-analytics.tsx"],
  "app/(admin)/counts/analytics/loading.tsx": ["features/counts/herd-analytics.tsx"],
  "app/(admin)/counts/breakdown/loading.tsx": ["features/counts/counts-breakdown.tsx"],
  "app/(admin)/counts/herd/loading.tsx": ["features/counts/herd-register.tsx"],
  "app/(admin)/counts/mortality/loading.tsx": ["features/counts/mortality.tsx"],
  "app/(admin)/counts/milk-preparation/loading.tsx": ["features/counts/milk-preparation.tsx"],
  "features/procurement/vendor-skeletons.tsx": ["features/procurement/vendor-board.tsx"],
  "features/procurement/feed-purchases-skeletons.tsx": ["features/procurement/feed-purchases.tsx"],
  "app/(admin)/operations/audit/loading.tsx": ["features/operations-audit/audit-log.tsx"],
  "app/(admin)/operations/dlq/loading.tsx": ["features/operations-dlq/index.tsx"],
  "app/(admin)/leave/loading.tsx": ["features/leave/leave-page.tsx"],
  "features/procurement/source-entry-skeletons.tsx": ["features/procurement/source-entry-board.tsx"],
  "features/vaccination-live-tracker/live-tracker-skeleton.tsx": ["features/vaccination-live-tracker/live-tracker-board.tsx"],
};

/** Whether each `crumbs={[{ … }` literal's first (parent) crumb carries an href. */
export function parentCrumbLinks(src) {
  return [...src.matchAll(/crumbs=\{\[\s*\{([^{}]*(?:\([^()]*\)[^{}]*)*)\}/g)].map((m) => /\bhref\s*:/.test(m[1]));
}

/** The skeleton's crumbLink (default true), per PageHeaderSkeleton use. */
export function skeletonCrumbLinks(src) {
  return [...src.matchAll(/<PageHeaderSkeleton\b([^>]*)\/>/g)].map((m) => !/crumbLink=\{false\}/.test(m[1]));
}

test("self-test: parent crumb href detection", () => {
  assert.deepEqual(parentCrumbLinks('crumbs={[{ label: copy(c, "crumb"), href: "/counts/herd" }, { label: t }]}'), [true]);
  assert.deepEqual(parentCrumbLinks('crumbs={[{ label: copy(c, "crumb") }, { label: t }]}'), [false]);
  assert.deepEqual(skeletonCrumbLinks("<PageHeaderSkeleton crumbLink={false} titleWidth={1} />"), [false]);
  assert.deepEqual(skeletonCrumbLinks("<PageHeaderSkeleton actionWidths={[1]} />"), [true]);
});

test("crumb-link-truth: each twin's crumbLink equals its page's parent crumb", () => {
  for (const [skel, pages] of Object.entries(TWINS)) {
    const pageLinks = pages.flatMap((p) => parentCrumbLinks(read(p)));
    assert.ok(pageLinks.length > 0, `${pages.join(", ")}: no crumbs={[...]} literal found`);
    assert.ok(pageLinks.every((l) => l === pageLinks[0]), `${pages.join(", ")}: its PageHeaders disagree on the parent crumb link`);
    const skelLinks = skeletonCrumbLinks(read(skel));
    assert.ok(skelLinks.length > 0, `${skel}: no PageHeaderSkeleton`);
    for (const l of skelLinks) assert.equal(l, pageLinks[0], `${skel}: crumbLink must be ${pageLinks[0]} (the page's parent crumb ${pageLinks[0] ? "links" : "has no href"})`);
  }
});

test("crumb-link-truth: every crumbLink={false} is covered by the table", () => {
  const walk = (dir) => readdirSync(dir).flatMap((n) => {
    const full = join(dir, n);
    if (n === "node_modules" || n.startsWith(".")) return [];
    return statSync(full).isDirectory() ? walk(full) : /\.tsx$/.test(n) ? [full] : [];
  });
  const users = ["app", "features", "components"].flatMap((d) => walk(join(root, d))).filter((f) => readFileSync(f, "utf8").includes("crumbLink={false}")).map((f) => relative(root, f));
  for (const u of users) assert.ok(u in TWINS, `${u} sets crumbLink={false}: add it to TWINS with its page file`);
});
