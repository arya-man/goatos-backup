import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

// guard: sk2-skeleton-twins (REVIEW-47). The operations / procurement twins take
// every layout number (tab lists, column lists, field widths, page sizes) from the route's
// *-layout.ts, which the page imports too; list constants mirror the backend page contract.
const read = (p) => readFileSync(new URL(p, import.meta.url), "utf8");
const service = read("../../../../backend/internal/adminui/app/service.go");
const contractColumns = (id) => {
  const m = new RegExp(`table[P]?\\("${id}",[^\\n]*?\\[\\]string\\{([^}]*)\\}`).exec(service) ?? new RegExp(`table[P]?\\("${id}",[^\\n]*\\n\\s*\\[\\]string\\{([^}]*)\\}`).exec(service);
  assert.ok(m, `contract table ${id}`);
  return [...m[1].matchAll(/"([^"]+)"/g)].map((x) => x[1]);
};
const contractOptions = (id) => {
  const start = service.indexOf(`ID: "${id}"`);
  assert.ok(start >= 0, `option group ${id}`);
  const block = service.slice(start, service.indexOf("},\n", service.indexOf("Options:", start)));
  return [...block.matchAll(/option\("([^"]+)"/g)].map((x) => x[1]);
};
const constList = (src, name) => {
  const m = new RegExp(`export const ${name} = \\[([^\\]]*)\\]`).exec(src);
  assert.ok(m, name);
  return [...m[1].matchAll(/"([^"]+)"/g)].map((x) => x[1]);
};

test("layout lists mirror the backend page contract", () => {
  const audit = read("../operations-audit/audit-layout.ts");
  const dlq = read("../operations-dlq/dlq-layout.ts");
  const feed = read("./feed-purchases-layout.ts");
  const source = read("./source-entry-layout.ts");
  assert.deepEqual(constList(audit, "AUDIT_TRAIL_COLUMNS"), contractColumns("activity-trail"));
  assert.deepEqual(constList(dlq, "DLQ_EVENT_COLUMNS"), contractColumns("dlq-events"));
  assert.deepEqual(constList(feed, "FEED_PURCHASE_COLUMNS"), contractColumns("feed-purchases"));
  assert.deepEqual(constList(feed, "FEED_DELIVERY_STATUS_KEYS"), contractOptions("feed_purchase_delivery_statuses"));
  assert.deepEqual(constList(source, "SOURCE_LOAD_COLUMNS"), contractColumns("source-loads"));
  assert.deepEqual(constList(source, "SOURCE_LOAD_STATUS_KEYS"), contractOptions("source_load_status"));
  const animals = read("./animal-purchases-layout.ts");
  assert.deepEqual(constList(animals, "ANIMAL_LOAD_COLUMNS"), contractColumns("animal-purchase-loads"));
  assert.deepEqual(constList(animals, "ANIMAL_DECISION_KEYS"), contractOptions("animal_purchase_decisions"));
});

test("pages read their tab lists, widths and page sizes from the same layout files", () => {
  const auditPage = read("../operations-audit/audit-log.tsx");
  assert.match(auditPage, /const STATUS_TABS = AUDIT_STATUS_TABS;/);
  assert.match(auditPage, /minWidth=\{AUDIT_FAMILY_SELECT_WIDTH\}/);
  const dlqPage = read("../operations-dlq/index.tsx");
  assert.match(dlqPage, /const STATUS_KEYS = DLQ_STATUS_KEYS;/);
  assert.equal((dlqPage.match(/md: DLQ_FIELD_WIDTH/g) ?? []).length, 2);
  assert.match(read("./feed-purchases.tsx"), /import \{ DEFAULT_DELIVERY, DEFAULT_LIMIT[^}]*\} from "\.\/feed-purchases-layout";/);
});

test("the twins retype no layout number", () => {
  const twins = {
    "operations/audit": read("../../app/(admin)/operations/audit/loading.tsx"),
    "operations/dlq": read("../../app/(admin)/operations/dlq/loading.tsx"),
    "feed-purchases": read("./feed-purchases-skeletons.tsx"),
    "source-entry": read("./source-entry-skeletons.tsx"),
    "animal-purchases": read("./animal-purchases-skeletons.tsx"),
  };
  for (const [name, src] of Object.entries(twins)) {
    assert.match(src, /-layout"/, `${name} imports its layout file`);
    assert.doesNotMatch(src, /(columns|rows|count)=\{\d+\}|fields=\{\[\d|trailing=\{\[\d|buttons=\{\[\d/, `${name}: a layout number is retyped`);
  }
  assert.match(read("./source-entry-board.tsx"), /fallback=\{<SourceLoadRowsSkeleton /);
  const animalPage = read("./animal-purchases.tsx");
  assert.match(animalPage, /fallback=\{<AnimalLoadRowsSkeleton rows=\{loadsLimit\} \/>\}/);
  assert.match(animalPage, /fallback=\{<AnimalCardsSkeleton \/>\}/);
  assert.match(animalPage, /size=\{ANIMAL_KPI_SIZE\}/);
  assert.match(read("../../app/(admin)/procurement/animal-purchases/loading.tsx"), /<AnimalPurchasesSkeleton \/>/);
  assert.match(read("../../app/(admin)/procurement/source-entry/loading.tsx"), /<SourceEntrySkeleton \/>/);
});

// REVIEW-49 O77 / REVIEW-50 O82.
test("pages consume the layout constants their twins count; the load detail twins OrderDetailsToolbar", () => {
  const audit = read("../operations-audit/audit-log.tsx");
  assert.match(audit, /size=\{AUDIT_SIDE_GRID\.operators\}/);
  assert.match(audit, /size=\{AUDIT_SIDE_GRID\.advanced\}/);
  assert.doesNotMatch(audit, /size=\{\{ xs: 12, md: [57] \}\}/);
  const feed = read("./feed-purchases.tsx");
  assert.match(feed, /FEED_STRIP_CELLS\.map\(/);
  assert.match(feed, /filters=\{FEED_TOOLBAR_FILTERS\.map\(/);
  const feedTwin = read("./feed-purchases-skeletons.tsx");
  assert.match(feedTwin, /count=\{FEED_STRIP_CELLS\.length\}/);
  assert.match(feedTwin, /filters=\{FEED_TOOLBAR_FILTERS\.length\}/);
  assert.match(read("./load-detail.tsx"), /<OrderDetailsToolbar\b/);
  const loadLoading = read("../../app/(admin)/procurement/source-entry/loads/[load_id]/loading.tsx");
  assert.match(loadLoading, /<OrderDetailsToolbarSkeleton \/>/);
  assert.doesNotMatch(loadLoading, /PageHeaderSkeleton/);
});
