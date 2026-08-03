// Pure week/park selection for the weighing planner. It lives outside data.ts because
// data.ts is "server-only" and therefore cannot be exercised by a unit test, while this
// rule (which campaign does a park switch land on?) is exactly the part that regressed.

export interface WeighingCampaignSelectionItem {
  campaign_id: string;
  park_id: string;
  period_start_date: string;
}

/**
 * weekStartOfDay returns the Monday of the week containing a "YYYY-MM-DD" Mesha business day.
 *
 * It takes an already-resolved business day rather than an instant because the previous version
 * derived the week from the UTC calendar date. Between 00:00 and 05:29 IST every Monday, UTC is
 * still on Sunday, so "this week" resolved to LAST week -- which handed the planner a past week and
 * therefore the edit/duplicate-blocked state that made creating the real current-week task
 * impossible. The single timezone conversion belongs to `todayIso()` in lib/format, and this
 * function must not re-enter one: `Date.UTC` is used purely as a month/year-rollover calculator on
 * the bare Y-M-D, never as a clock (the same convention `istDayPlus` documents).
 */
export function weekStartOfDay(day: string): string {
  const [year, month, date] = day.split("-").map(Number);
  if (!Number.isFinite(year) || !Number.isFinite(month) || !Number.isFinite(date)) return day;
  const anchor = new Date(Date.UTC(year, month - 1, date));
  const weekday = anchor.getUTCDay();
  anchor.setUTCDate(anchor.getUTCDate() + (weekday === 0 ? -6 : 1 - weekday));
  const pad = (value: number) => String(value).padStart(2, "0");
  return `${anchor.getUTCFullYear()}-${pad(anchor.getUTCMonth() + 1)}-${pad(anchor.getUTCDate())}`;
}

export function selectCampaign<T extends WeighingCampaignSelectionItem>(
  items: T[],
  // Leads the optional selectors because it is REQUIRED, and is deliberately not defaulted from a
  // runtime clock here: the caller owns the IST business day, so this module cannot silently
  // reintroduce a UTC-derived week the way the old trailing default did.
  thisWeekStart: string,
  selectedWeek?: string,
  selectedCampaignId?: string,
  selectedParkId?: string,
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
