import assert from "node:assert/strict";
import test from "node:test";

import { currentWeekStart, selectCampaign } from "./campaign-selection.ts";

const THIS_WEEK = "2026-08-03";

function campaign(overrides) {
  return {
    campaign_id: "c-old",
    park_id: "cbe",
    period_start_date: "2026-05-04",
    ...overrides,
  };
}

test("currentWeekStart returns the Monday of the week in UTC", () => {
  assert.equal(currentWeekStart(new Date("2026-08-06T11:00:00Z")), "2026-08-03");
  assert.equal(currentWeekStart(new Date("2026-08-03T00:00:00Z")), "2026-08-03");
  assert.equal(currentWeekStart(new Date("2026-08-09T23:00:00Z")), "2026-08-03");
});

test("a park holding only historical campaigns lands on nothing, not on the oldest week", () => {
  const items = [
    campaign({ campaign_id: "cpt-old-1", park_id: "cpt", period_start_date: "2026-03-02" }),
    campaign({ campaign_id: "cpt-old-2", park_id: "cpt", period_start_date: "2026-04-06" }),
    campaign({ campaign_id: "cbe-now", park_id: "cbe", period_start_date: THIS_WEEK }),
  ];

  const selected = selectCampaign(items, undefined, undefined, "cpt", THIS_WEEK);

  assert.equal(selected, undefined);
});

test("a park with a current-week campaign selects that campaign on a plain park switch", () => {
  const items = [
    campaign({ campaign_id: "cpt-old", park_id: "cpt", period_start_date: "2026-03-02" }),
    campaign({ campaign_id: "cpt-now", park_id: "cpt", period_start_date: THIS_WEEK }),
  ];

  const selected = selectCampaign(items, undefined, undefined, "cpt", THIS_WEEK);

  assert.equal(selected?.campaign_id, "cpt-now");
});

test("an explicit week still reaches that park's historical campaign", () => {
  const items = [
    campaign({ campaign_id: "cpt-old", park_id: "cpt", period_start_date: "2026-03-02" }),
    campaign({ campaign_id: "cpt-now", park_id: "cpt", period_start_date: THIS_WEEK }),
  ];

  const selected = selectCampaign(items, "2026-03-02", undefined, "cpt", THIS_WEEK);

  assert.equal(selected?.campaign_id, "cpt-old");
});

test("a campaign id from another park never wins while a park is selected", () => {
  const items = [
    campaign({ campaign_id: "cbe-now", park_id: "cbe", period_start_date: THIS_WEEK }),
    campaign({ campaign_id: "cpt-now", park_id: "cpt", period_start_date: THIS_WEEK }),
  ];

  const selected = selectCampaign(items, undefined, "cbe-now", "cpt", THIS_WEEK);

  assert.equal(selected?.campaign_id, "cpt-now");
});

test("an explicit week that the selected park lacks falls to the current week, not a stale row", () => {
  const items = [
    campaign({ campaign_id: "cpt-old", park_id: "cpt", period_start_date: "2026-03-02" }),
    campaign({ campaign_id: "cpt-now", park_id: "cpt", period_start_date: THIS_WEEK }),
  ];

  const selected = selectCampaign(items, "2026-06-01", undefined, "cpt", THIS_WEEK);

  assert.equal(selected?.campaign_id, "cpt-now");
});

test("with no park on the URL the first campaign is still the default", () => {
  const items = [campaign({ campaign_id: "first" }), campaign({ campaign_id: "second" })];

  assert.equal(selectCampaign(items, undefined, undefined, undefined, THIS_WEEK)?.campaign_id, "first");
});
