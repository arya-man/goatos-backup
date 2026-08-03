import assert from "node:assert/strict";
import test from "node:test";

import { selectCampaign, weekStartOfDay } from "./campaign-selection.ts";

const THIS_WEEK = "2026-08-03";

function campaign(overrides) {
  return {
    campaign_id: "c-old",
    park_id: "cbe",
    period_start_date: "2026-05-04",
    ...overrides,
  };
}

test("weekStartOfDay returns the Monday of the week containing a business day", () => {
  assert.equal(weekStartOfDay("2026-08-06"), "2026-08-03");
  assert.equal(weekStartOfDay("2026-08-03"), "2026-08-03");
  assert.equal(weekStartOfDay("2026-08-09"), "2026-08-03");
  // Month and year rollover: the Monday of a week can sit in the previous month/year.
  assert.equal(weekStartOfDay("2026-03-01"), "2026-02-23");
  assert.equal(weekStartOfDay("2027-01-01"), "2026-12-28");
});

test("weekStartOfDay takes the IST business day, so early Monday morning is not last week", () => {
  // 00:30 IST on Monday 2026-08-03 is still Sunday 2026-08-02 in UTC. A UTC-derived week resolved
  // to 2026-07-27 there, dropping the planner into a past week and its duplicate-blocked state.
  const earlyMondayIst = new Date("2026-08-02T19:00:00Z");
  const istDay = new Intl.DateTimeFormat("en-CA", {
    timeZone: "Asia/Kolkata",
    year: "numeric",
    month: "2-digit",
    day: "2-digit",
  }).format(earlyMondayIst);

  assert.equal(istDay, "2026-08-03");
  assert.equal(weekStartOfDay(istDay), "2026-08-03");
});

test("a park holding only historical campaigns lands on nothing, not on the oldest week", () => {
  const items = [
    campaign({ campaign_id: "cpt-old-1", park_id: "cpt", period_start_date: "2026-03-02" }),
    campaign({ campaign_id: "cpt-old-2", park_id: "cpt", period_start_date: "2026-04-06" }),
    campaign({ campaign_id: "cbe-now", park_id: "cbe", period_start_date: THIS_WEEK }),
  ];

  const selected = selectCampaign(items, THIS_WEEK, undefined, undefined, "cpt");

  assert.equal(selected, undefined);
});

test("a park with a current-week campaign selects that campaign on a plain park switch", () => {
  const items = [
    campaign({ campaign_id: "cpt-old", park_id: "cpt", period_start_date: "2026-03-02" }),
    campaign({ campaign_id: "cpt-now", park_id: "cpt", period_start_date: THIS_WEEK }),
  ];

  const selected = selectCampaign(items, THIS_WEEK, undefined, undefined, "cpt");

  assert.equal(selected?.campaign_id, "cpt-now");
});

test("an explicit week still reaches that park's historical campaign", () => {
  const items = [
    campaign({ campaign_id: "cpt-old", park_id: "cpt", period_start_date: "2026-03-02" }),
    campaign({ campaign_id: "cpt-now", park_id: "cpt", period_start_date: THIS_WEEK }),
  ];

  const selected = selectCampaign(items, THIS_WEEK, "2026-03-02", undefined, "cpt");

  assert.equal(selected?.campaign_id, "cpt-old");
});

test("a campaign id from another park never wins while a park is selected", () => {
  const items = [
    campaign({ campaign_id: "cbe-now", park_id: "cbe", period_start_date: THIS_WEEK }),
    campaign({ campaign_id: "cpt-now", park_id: "cpt", period_start_date: THIS_WEEK }),
  ];

  const selected = selectCampaign(items, THIS_WEEK, undefined, "cbe-now", "cpt");

  assert.equal(selected?.campaign_id, "cpt-now");
});

test("an explicit week that the selected park lacks falls to the current week, not a stale row", () => {
  const items = [
    campaign({ campaign_id: "cpt-old", park_id: "cpt", period_start_date: "2026-03-02" }),
    campaign({ campaign_id: "cpt-now", park_id: "cpt", period_start_date: THIS_WEEK }),
  ];

  const selected = selectCampaign(items, THIS_WEEK, "2026-06-01", undefined, "cpt");

  assert.equal(selected?.campaign_id, "cpt-now");
});

test("with no park on the URL the first campaign is still the default", () => {
  const items = [campaign({ campaign_id: "first" }), campaign({ campaign_id: "second" })];

  assert.equal(selectCampaign(items, THIS_WEEK, undefined, undefined, undefined)?.campaign_id, "first");
});
