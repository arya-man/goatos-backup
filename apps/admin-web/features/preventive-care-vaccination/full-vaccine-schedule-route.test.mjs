import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

const source = readFileSync(
  new URL("./full-vaccine-schedule.tsx", import.meta.url),
  "utf8",
);
const drawerSource = readFileSync(
  new URL("./full-vaccine-schedule-drawer.tsx", import.meta.url),
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

test("vaccination schedule opens its shed drawer locally and toggles vaccine overflow locally", () => {
  assert.match(source, /LocalOverlayLink/);
  assert.match(source, /#schedule_event=/);
  assert.equal(
    source.includes('name="schedule_event"'),
    false,
    "Opening/searching the shed drawer must not submit route query state.",
  );
  assert.equal(
    /<LocalOverlayLink[^>]*title=\{vaccineTitle\}/.test(source),
    false,
    "Vaccine overflow must not open the unrelated shed drawer.",
  );
  assert.match(source, /<details className="schedule-vaccine-overflow">/);
  assert.match(source, /<summary[\s\S]*?title=\{vaccineTitle\}[\s\S]*?schedule-more-chip/);
  assert.match(source, /vaccines\.slice\(VACCINE_CHIP_PREVIEW_LIMIT\)\.map/);
  assert.match(source, /className="schedule-vaccine-overflow-list"/);
  assert.match(css, /\.schedule-vaccine-overflow>summary\{[^}]*cursor:pointer/);
});

test("vaccination schedule drawer supports real close and roster drilldown", () => {
  assert.match(drawerSource, /schedule-drawer-close-layer/);
  assert.match(drawerSource, /currentHistoryEntryIsLocalOverlay/);
  assert.match(drawerSource, /replaceLocalOverlayUrl/);
  assert.match(drawerSource, /setDrawerOpen\(false\)/);
  assert.match(drawerSource, /setDisplayedRow\(undefined\)/);
  assert.match(drawerSource, /}, 280\)/);
  assert.match(drawerSource, /translateX\(100%\)/);
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
