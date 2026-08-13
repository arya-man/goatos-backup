import { Activity, AlertTriangle, CalendarDays } from "lucide-react";
import Link from "@/components/no-prefetch-link";
import { getVaccinationLiveTracker, type ApiResult, type VaccinationLiveTrackerResponse } from "@/lib/api/server";
import { copy, optionGroup, tableLabels, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import { scopeHref } from "@/lib/scope";
import { one, type RouteSearchParams } from "@/lib/search-params";
import type { LiveTrackerShedRow } from "@/lib/api/vaccination-live-tracker";
import { liveTrackerHref, liveTrackerResetHref, parseLiveTrackerParams } from "./params";
import { LiveTrackerKpis } from "./live-tracker-kpis";
import { LiveTrackerOperators } from "./live-tracker-operators";
import { LiveTrackerSheds } from "./live-tracker-sheds";
import { LiveTrackerComboCard } from "./live-tracker-combo";
import { LiveTrackerRail } from "./live-tracker-rail";
import { LiveTrackerPassportDrawer } from "./live-tracker-passport-drawer";
import { LiveTrackerFilters, type LiveFilterSpec } from "./live-tracker-filters";
import { LivePoller } from "./live-poller";
import { fmtDriveDay } from "./format";

// The id rendered by features/preventive-care-vaccination/full-vaccine-schedule.tsx. Spelled once,
// asserted against that file in live-tracker.test.mjs.
export const FULL_SCHEDULE_ANCHOR = "full-schedule";

export function loadLiveTracker(searchParams: RouteSearchParams | undefined): Promise<ApiResult<VaccinationLiveTrackerResponse>> {
  const params = parseLiveTrackerParams(searchParams);
  return getVaccinationLiveTracker({
    businessDate: params.businessDate,
    parkId: params.parkId,
    shedId: params.shedId,
    partitionLabel: params.partitionLabel,
    operatorId: params.operatorId,
    vaccineCode: params.vaccineCode,
    status: params.status,
    activityLimit: params.activityLimit,
    activityBefore: params.activityBefore,
    activityBeforeId: params.activityBeforeId,
  });
}

export function LiveTrackerSkeleton({ pageContract }: { pageContract: AdminUiPageContract }) {
  const operatorCols = tableLabels(pageContract, "live-operators");
  return (
    <div className="lt-page" aria-busy="true">
      <div className="lt-kpis">
        {Array.from({ length: 6 }, (_, index) => (
          <div key={index} className="kpi lt-kpi">
            <div className="skel" style={{ width: 92, height: 12 }} />
            <div className="skel" style={{ width: 64, height: 26, marginTop: 8 }} />
            <div className="skel" style={{ width: 118, height: 11, marginTop: 6 }} />
          </div>
        ))}
      </div>
      <div className="lt-grid">
        <div className="lt-stack">
          <section className="card lt-card">
            <div className="hd">
              <div className="skel" style={{ width: 160, height: 16 }} />
            </div>
            <div className="bd lt-tablewrap">
              <table className="lt-operator-table">
                <thead>
                  <tr>
                    {operatorCols.map((label) => (
                      <th key={label}>{label}</th>
                    ))}
                  </tr>
                </thead>
                <tbody>
                  {Array.from({ length: 5 }, (_, row) => (
                    <tr key={row}>
                      {operatorCols.map((label, index) => (
                        <td key={label}>
                          <span className="skel" style={{ width: index < 3 ? 116 : 48, height: 16 }} />
                        </td>
                      ))}
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          </section>
        </div>
        <div className="lt-stack">
          <section className="card lt-card">
            <div className="hd">
              <div className="skel" style={{ width: 120, height: 16 }} />
            </div>
            <div className="bd">
              {Array.from({ length: 5 }, (_, row) => (
                <div key={row} className="skel" style={{ display: "block", width: "100%", height: 34, marginBottom: 8 }} />
              ))}
            </div>
          </section>
        </div>
      </div>
    </div>
  );
}

// The live drive tracker board.
//
// Three render branches are mandatory and all three are here: a failed read shows a visible error
// card carrying the backend message; an empty-but-ok read shows EVERY section at zero with a reset
// link; otherwise the full board. Collapsing an empty day into one billboard hides which of the six
// sections is actually empty, which is the whole diagnostic value of the page.
export async function LiveTrackerBoard({
  searchParams,
  pageContract,
  result: injectedResult,
}: {
  searchParams?: RouteSearchParams;
  pageContract: AdminUiPageContract;
  result?: ApiResult<VaccinationLiveTrackerResponse>;
}) {
  const params = parseLiveTrackerParams(searchParams);
  const result = injectedResult ?? (await loadLiveTracker(searchParams));
  const selectedGoatId = one(params.sp, "goat_passport");

  // The fragment must match the id the schedule section actually renders
  // (features/preventive-care-vaccination/full-vaccine-schedule.tsx: <section id="full-schedule">).
  // "full-vaccine-schedule" is the backend table-contract id and carries no DOM element, so the
  // button navigated to /vaccination and scrolled nowhere. live-tracker.test.mjs pins the two
  // together so this cannot rot again.
  const scheduleHref = scopeHref("/vaccination", params.scope, {}, {}) + "#" + FULL_SCHEDULE_ANCHOR;
  const commandHref = scopeHref("/vaccination", params.scope, {}, {});
  // /verify reads parseScope, and the verification counts on this board are park-scoped, so the
  // hand-off has to carry the same scope or the two screens disagree about the same queue.
  const verifyHref = scopeHref("/verify", params.scope, {}, {});

  if (!result.ok) {
    return (
      <div className="lt-page">
        {/* activeParks is null, not 0. The read FAILED, so nothing is known about how many parks are
            running; rendering a hard zero under a pulsing LIVE badge asserts that none are, which is
            a measurement this branch does not have. */}
        <PageHead
          pageContract={pageContract}
          businessDate={params.businessDate ?? ""}
          activeParks={null}
          isLiveDay={false}
          generatedAt={null}
          scheduleHref={scheduleHref}
          commandHref={commandHref}
        />
        <section className="card lt-card">
          <div className="bd lt-empty">
            <AlertTriangle className="ic" aria-hidden="true" style={{ width: 18, height: 18, color: "var(--danger)", flexShrink: 0 }} />
            <div style={{ minWidth: 0, flex: 1 }}>
              <b style={{ fontSize: 14 }}>{copy(pageContract, "state.error_title")}</b>
              <span className="muted small" style={{ display: "block", marginTop: 2, lineHeight: 1.5 }}>
                {copy(pageContract, "state.error_body")} {result.error.message}
              </span>
              {/* LivePoller unmounts on a failed read (it needs a real generated_at to report), and
                  router.refresh() is this page's only refresh path. Without a retry control one
                  transient read failure — a 6s API timeout is a realistic way in — freezes the board
                  until the reader manually reloads the browser. */}
              <Link href={liveTrackerHref(params)} replace className="btn sm" style={{ marginTop: 8 }}>
                {copy(pageContract, "action.retry")}
              </Link>
            </div>
          </div>
        </section>
      </div>
    );
  }

  const data = result.data;
  const asOfDate = /^\d{4}-\d{2}-\d{2}/.test(params.scope.asOf ?? "") ? params.scope.asOf!.slice(0, 10) : null;
  const resetHref = liveTrackerResetHref(params);
  const filters = buildFilters(params, data, pageContract);
  // hasFilter deliberately excludes the top-bar park scope, and liveTrackerResetHref re-emits that
  // scope, so offering "clear all" for a bare park selection produced a button that navigated to the
  // identical URL and changed nothing.
  const clearAllHref = params.hasFilter ? resetHref : null;
  const passportHref = (goatId: string) => `${liveTrackerHref(params, { goat_passport: goatId })}#lt-combo`;
  const closePassportHref = `${liveTrackerHref(params)}#lt-combo`;
  const shedHref = (row: LiveTrackerShedRow) =>
    scopeHref(
      `/vaccination/execution/sheds/${encodeURIComponent(row.shed_id)}`,
      params.scope,
      row.park_id ? { mode: "park" as const, park: row.park_id } : {},
      { ret: liveTrackerHref(params) },
    );

  const isEmpty =
    data.kpis.scheduled_administrations === 0 &&
    data.operators.length === 0 &&
    data.sheds.length === 0 &&
    data.activity.items.length === 0;

  return (
    <div className="lt-page">
      <PageHead
        pageContract={pageContract}
        businessDate={data.business_date}
        activeParks={data.kpis.active_parks}
        isLiveDay={data.is_live_day}
        generatedAt={data.generated_at}
        scheduleHref={scheduleHref}
        commandHref={commandHref}
      />

      <LiveTrackerFilters
        filters={filters}
        clearAllHref={clearAllHref}
        optionsTruncated={data.filter_options.truncated}
        pageContract={pageContract}
      />

      {/* The top bar's as_of travels into every nav leaf including this one, but this surface is a
          DRIVE DAY board keyed on business_date — it does not honour as_of, and the sibling
          vaccination reads reject a past as_of outright. Silently answering with a different day
          than the URL claims is the failure mode; saying which day is on screen is the fix. */}
      {asOfDate && asOfDate !== data.business_date ? (
        <div className="note lt-truncnote" role="status">
          {copy(pageContract, "label.as_of_note")}
        </div>
      ) : null}

      {/* The wording turns on hasNarrowing, which INCLUDES the top-bar park. "No vaccination drive
          work on this day" is a claim about the whole day; with a park selected it is false whenever
          the other park is running, and the reader is given no hint that a scope is even active. */}
      {isEmpty ? (
        <div className="note lt-emptynote">
          <b>
            {params.hasNarrowing
              ? copy(pageContract, "state.empty_filtered_title")
              : copy(pageContract, "state.empty_title")}
          </b>
          <span className="muted small" style={{ display: "block", marginTop: 3, lineHeight: 1.5 }}>
            {params.hasNarrowing
              ? copy(pageContract, "state.empty_filtered_body")
              : copy(pageContract, "state.empty_body")}
          </span>
          {params.hasFilter ? (
            <Link href={resetHref} replace scroll={false} className="btn sm" style={{ marginTop: 8 }}>
              {copy(pageContract, "action.reset_filters")}
            </Link>
          ) : null}
        </div>
      ) : null}

      <LiveTrackerKpis kpis={data.kpis} truncated={data.cells_truncated} pageContract={pageContract} />

      <div className="lt-grid">
        <div className="lt-stack">
          <LiveTrackerOperators
            rows={data.operators}
            parkCount={data.kpis.scheduled_by_park.length}
            total={data.operators_total}
            truncated={data.operators_truncated}
            unassignedAdmins={data.unassigned_administrations}
            hasFilter={params.hasFilter}
            resetHref={resetHref}
            pageContract={pageContract}
          />
          <LiveTrackerSheds
            rows={data.sheds}
            total={data.sheds_total}
            truncated={data.sheds_truncated}
            hasFilter={params.hasFilter}
            resetHref={resetHref}
            shedHref={shedHref}
            pageContract={pageContract}
          />
          {/* No truncatedHref: there is no paginated combo-animal list to point at, and wiring the
              all-combo-animals control to the FIRST row's passport drawer would be an affordance
              that silently does something other than what it says. The card renders that branch
              disabled with its own visible reason instead. */}
          <LiveTrackerComboCard combo={data.combo} passportHref={passportHref} pageContract={pageContract} />
        </div>
        <LiveTrackerRail
          activity={data.activity}
          attention={data.attention}
          attentionTotal={data.attention_total}
          attentionTruncated={data.attention_truncated}
          verification={data.verification}
          verifyHref={verifyHref}
          pageContract={pageContract}
        />
      </div>

      <LiveTrackerPassportDrawer
        rows={data.combo.rows}
        initialSelectedGoatId={selectedGoatId}
        closeHref={closePassportHref}
        pageContract={pageContract}
      />
    </div>
  );
}

function PageHead({
  pageContract,
  businessDate,
  activeParks,
  isLiveDay,
  generatedAt,
  scheduleHref,
  commandHref,
}: {
  pageContract: AdminUiPageContract;
  // null = the read failed, so the number is UNKNOWN. It is not zero.
  activeParks: number | null;
  businessDate: string;
  isLiveDay: boolean;
  generatedAt: string | null;
  scheduleHref: string;
  commandHref: string;
}) {
  const dayLabel = businessDate ? fmtDriveDay(businessDate) : copy(pageContract, "label.placeholder");
  return (
    <div className="phead lt-phead">
      <div>
        <div className="crumb">
          {copy(pageContract, "crumb")} / <b>{copy(pageContract, "page.title")}</b>
        </div>
        <h1 style={{ margin: 0, fontSize: 21, letterSpacing: "-.4px" }}>
          {copy(pageContract, "page.heading_prefix")} — {dayLabel}
          {/* A past drive day is a legitimate read here, and the rest of the page is already
              clock-corrected for it. The chip was not: a drive that closed days ago rendered "1 park
              running" under a pulsing LIVE badge. Past days get a neutral, past-tense chip. */}
          {activeParks == null ? null : (
            <span
              className={`tag ${isLiveDay ? "t-live" : "t-mut"}`}
              style={{ verticalAlign: "middle", marginLeft: 6 }}
            >
              {isLiveDay ? <i /> : null}
              {activeParks}{" "}
              {isLiveDay
                ? copy(pageContract, activeParks === 1 ? "chip.parks_running_one" : "chip.parks_running_many")
                : copy(pageContract, activeParks === 1 ? "chip.parks_active_one" : "chip.parks_active_many")}
            </span>
          )}
        </h1>
        <div className="sub">{copy(pageContract, "page.subtitle")}</div>
      </div>
      <div className="sp" style={{ flex: 1 }} />
      <div className="lt-headactions">
        {/* The mock's own top bar carried the LIVE badge, clock and interval picker. Brand, park
            scope, date scope and theme already live in the app shell, so only the live controls move
            here — every element still appears, just hosted by the surface that owns it. */}
        {generatedAt && isLiveDay ? <LivePoller generatedAt={generatedAt} pageContract={pageContract} /> : null}
        <div className="lt-headbtns">
          <Link href={scheduleHref} className="btn">
            <CalendarDays className="ic" style={{ width: 14, height: 14 }} aria-hidden="true" />
            {copy(pageContract, "action.full_schedule")}
          </Link>
          <Link href={commandHref} className="btn p">
            <Activity className="ic" style={{ width: 14, height: 14 }} aria-hidden="true" />
            {copy(pageContract, "action.command_board")}
          </Link>
        </div>
      </div>
      <span className="lt-scopenote muted small">{copy(pageContract, "label.park_scope_note")}</span>
    </div>
  );
}

// The filter vocabulary is entirely server-authored: parks, vaccines, operators and sheds come from
// the response's own filter_options (compiled from the day's real administrations), and statuses from
// the page contract. An option that matches nothing therefore cannot be offered — the mock's dead
// "FMD + HS (combo)" choice, which hid the whole shed table when selected, is structurally impossible.
function buildFilters(
  params: ReturnType<typeof parseLiveTrackerParams>,
  data: VaccinationLiveTrackerResponse,
  pageContract: AdminUiPageContract,
): LiveFilterSpec[] {
  const statusOptions = optionGroup(pageContract, "live_status_filter");
  return [
    {
      id: "lt_park",
      label: copy(pageContract, "filter.park_label"),
      allLabel: copy(pageContract, "filter.all_parks"),
      icon: "layers",
      selected: params.parkId ?? "",
      // Park writes the SHARED top-bar scope key, not a second page-local park filter, so the shell
      // selector and this control can never disagree about which park is in view.
      choices: data.filter_options.parks.map((option) => ({
        value: option.id,
        label: option.label,
        href: liveTrackerHref(params, { park: option.id }),
      })),
      clearHref: liveTrackerHref(params, { park: undefined }),
    },
    {
      id: "lt_vaccine",
      label: copy(pageContract, "filter.vaccine_label"),
      allLabel: copy(pageContract, "filter.all_vaccines"),
      icon: "syringe",
      selected: params.vaccineCode ?? "",
      choices: data.filter_options.vaccines.map((option) => ({
        value: option.code,
        label: option.label,
        href: liveTrackerHref(params, { lt_vaccine: option.code }),
      })),
      clearHref: liveTrackerHref(params, { lt_vaccine: undefined }),
    },
    {
      id: "lt_operator",
      label: copy(pageContract, "filter.operator_label"),
      allLabel: copy(pageContract, "filter.all_operators"),
      icon: "user",
      selected: params.operatorId ?? "",
      choices: data.filter_options.operators.map((option) => ({
        value: option.id,
        label: option.label,
        href: liveTrackerHref(params, { lt_operator: option.id }),
      })),
      clearHref: liveTrackerHref(params, { lt_operator: undefined }),
    },
    {
      id: "lt_shed",
      label: copy(pageContract, "filter.shed_label"),
      allLabel: copy(pageContract, "filter.all_sheds"),
      selected: params.shedId && params.partitionLabel ? `${params.shedId}|${params.partitionLabel}` : "",
      choices: data.filter_options.sheds.map((option) => ({
        value: `${option.id}|${option.partition_label}`,
        label: option.label,
        href: liveTrackerHref(params, { lt_shed: option.id, lt_partition: option.partition_label }),
      })),
      clearHref: liveTrackerHref(params, { lt_shed: undefined, lt_partition: undefined }),
    },
    {
      id: "lt_status",
      label: copy(pageContract, "filter.status_label"),
      allLabel: copy(pageContract, "filter.all_statuses"),
      selected: params.status ?? "",
      choices: statusOptions.map((option) => ({
        value: option.key,
        label: option.label,
        href: liveTrackerHref(params, { lt_status: option.key }),
      })),
      clearHref: liveTrackerHref(params, { lt_status: undefined }),
    },
  ];
}
