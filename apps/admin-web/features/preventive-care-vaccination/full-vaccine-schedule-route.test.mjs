import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

const source = readFileSync(
  new URL("./full-vaccine-schedule.tsx", import.meta.url),
  "utf8",
);
const css = readFileSync(new URL("../../app/mesha-theme.css", import.meta.url), "utf8");
const adminUiContractSource = readFileSync(new URL("../../../../backend/internal/adminui/app/service.go", import.meta.url), "utf8");

test("vaccination full schedule interactions stay inside the vaccination IA", () => {
  assert.equal(
    source.includes("/calendar/drive/"),
    false,
    "Vaccination schedule cells must not deep-link to Calendar drive detail; use the vaccination-owned drawer/route.",
  );
  assert.match(source, /href=\{shedDrawerHref\(row\)\}/);
});

test("vaccination schedule drawer supports real close and roster drilldown", () => {
  assert.match(source, /schedule-drawer-close-layer/);
  assert.match(source, /\/vaccination\/execution\/sheds\/\$\{encodeURIComponent\(group\.shedId\)\}/);
  assert.match(source, /drive_due_date/);
  assert.match(source, /shed\.shed_id\s*\?\?\s*shed\.shedId/);
  assert.match(source, /shed\.total_animals\s*\?\?\s*shed\.totalAnimals/);
  assert.equal(
    /\.schedule-drawer-backdrop\{[^}]*pointer-events\s*:\s*none/.test(css),
    false,
    "Drawer backdrop must allow outside-click close; pointer-events:none makes it visually modal but not dismissible.",
  );
});

test("vaccination schedule table is visually bounded on desktop", () => {
  assert.equal(
    /\.full-vaccine-schedule-table\{[^}]*min-width\s*:\s*1280px/.test(css),
    false,
    "Full schedule table must not force right-edge columns off-screen at desktop widths.",
  );
  assert.match(css, /\.full-vaccine-schedule-table\{[^}]*width\s*:\s*100%/);
  assert.match(css, /\.full-vaccine-schedule-table th:nth-child\(6\)/);
});

test("vaccination schedule copy keys are backend-owned", () => {
  const requiredKeys = [
    "schedule.unit.animal",
    "schedule.unit.animals",
    "schedule.unit.dose",
    "schedule.unit.doses",
    "schedule.load.batches",
    "schedule.load.deferred_short",
    "schedule.load.scheduled_short",
    "schedule.load.single_drive",
    "schedule.drawer.title",
    "schedule.drawer.open_sheds",
    "schedule.drawer.open_roster",
    "schedule.drawer.more",
    "schedule.drawer.close",
    "schedule.drawer.search",
    "schedule.drawer.search_action",
    "schedule.drawer.previous_page",
    "schedule.drawer.next_page",
    "schedule.drawer.page_label",
    "schedule.drawer.rows_label",
    "schedule.drawer.empty",
  ];
  for (const key of requiredKeys) {
    assert.match(adminUiContractSource, new RegExp(`"${key.replaceAll(".", "\\.")}"\\s*:`), `${key} missing from backend UI contract`);
  }
});
