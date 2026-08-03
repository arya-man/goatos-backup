// Pure week/park selection for the weighing planner. It lives outside data.ts because
// data.ts is "server-only" and therefore cannot be exercised by a unit test, while this
// rule (which campaign does a park switch land on?) is exactly the part that regressed.

export interface WeighingCampaignSelectionItem {
  campaign_id: string;
  park_id: string;
  period_start_date: string;
}

/** currentWeekStart returns the Monday of the current UTC week as YYYY-MM-DD. */
export function currentWeekStart(now: Date = new Date()): string {
  const date = new Date(Date.UTC(now.getUTCFullYear(), now.getUTCMonth(), now.getUTCDate()));
  const day = date.getUTCDay();
  const diff = day === 0 ? -6 : 1 - day;
  date.setUTCDate(date.getUTCDate() + diff);
  return date.toISOString().slice(0, 10);
}

export function selectCampaign<T extends WeighingCampaignSelectionItem>(
  items: T[],
  selectedWeek?: string,
  selectedCampaignId?: string,
  selectedParkId?: string,
  thisWeekStart: string = currentWeekStart(),
): T | undefined {
  // A campaign from Park A must NEVER be selected as "the current campaign" while
  // Park B is displayed (W14). When an explicit park is on the URL, every lookup
  // below is scoped to that park's own campaigns only.
  const scoped = selectedParkId
    ? items.filter((item) => item.park_id === selectedParkId)
    : items;
  if (selectedCampaignId) {
    const byCampaign = scoped.find((item) => item.campaign_id === selectedCampaignId);
    if (byCampaign) return byCampaign;
    // selectedCampaignId belongs to a different park than the one on screen (or does
    // not exist) -- fall through to week/park-default lookup instead of ever
    // returning a cross-park campaign.
  }
  if (selectedWeek) {
    const byWeek = scoped.find((item) => item.period_start_date === selectedWeek);
    if (byWeek) return byWeek;
  }
  if (selectedParkId) {
    // Explicit park selected and nothing matched. Never default to another park's
    // campaign (items[0]), and never default to this park's FIRST row either: that row
    // is whatever the API happened to return first, so a park holding only historical
    // campaigns dragged the planner back to a past week and, because a selected campaign
    // makes the planner render the edit/duplicate-blocked state, the planner could no
    // longer create THIS week's task for that park. Landing on the current week (and on
    // nothing at all when the park has no campaign for it) keeps creation reachable.
    return scoped.find((item) => item.period_start_date === thisWeekStart);
  }
  return items[0];
}
