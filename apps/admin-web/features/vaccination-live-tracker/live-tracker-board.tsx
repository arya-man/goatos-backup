import Alert from "@mui/material/Alert";
import AlertTitle from "@mui/material/AlertTitle";
import { LT_BLOCK_MB, LT_HEADER_MB, LT_MAIN_SIZE, LT_RAIL_SIZE } from "./live-tracker-layout";
import Box from "@mui/material/Box";
import Button from "@mui/material/Button";
import Grid from "@mui/material/Grid";
import Stack from "@mui/material/Stack";
import { Iconify } from "@/components/minimal/iconify";
import { UrlSuspense } from "@/components/app/url-suspense";
import { LiveTrackerBodySkeleton } from "./live-tracker-skeleton";
import { ALL_PARAMS } from "@/components/app/url-tab-nav";
import Link from "@/components/no-prefetch-link";
import { getVaccinationLiveTracker, type ApiResult, type VaccinationLiveTrackerResponse } from "@/lib/api/server";
import { copy, optionGroup, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import { scopeHref } from "@/lib/scope";
import { one, type RouteSearchParams } from "@/lib/search-params";
import type { LiveTrackerShedRow } from "@/lib/api/vaccination-live-tracker";
import { liveTrackerHref, liveTrackerResetHref, parseLiveTrackerParams } from "./params";
import { vaccinationScheduleYear } from "@/features/preventive-care-vaccination";
import { PageHeader } from "@/components/app/page-header";
import { AppWelcome } from "@/components/app/sections/overview/app/app-welcome";
import { Label } from "@/components/minimal/label";
import { LiveTrackerKpis } from "./live-tracker-kpis";
import { LiveTrackerOperators } from "./live-tracker-operators";
import { LiveTrackerSheds } from "./live-tracker-sheds";
import { LiveTrackerComboCard } from "./live-tracker-combo";
import { LiveTrackerRail } from "./live-tracker-rail";
import { LiveTrackerPassportDrawer } from "./live-tracker-passport-drawer";
import { LiveTrackerFilters, type LiveFilterSpec } from "./live-tracker-filters";
import { LiveIntervalField, LivePoller } from "./live-poller";
import { fmtClockSeconds } from "./format";
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
  const scheduleHref =
    scopeHref("/vaccination", params.scope, {}, { view: "schedule", schedule_year: String(vaccinationScheduleYear(params.sp)) }) +
    "#" +
    FULL_SCHEDULE_ANCHOR;
  const commandHref = scopeHref("/vaccination", params.scope, {}, {});
  // /verify reads parseScope, and the verification counts on this board are park-scoped, so the
  // hand-off has to carry the same scope or the two screens disagree about the same queue.
  const verifyHref = scopeHref("/verify", params.scope, {}, {});

  if (!result.ok) {
    return (
      <Box>
        {/* activeParks is null, not 0. The read FAILED, so nothing is known about how many parks are
            running; rendering a hard zero under a pulsing LIVE badge asserts that none are, which is
            a measurement this branch does not have. */}
        <PageHead
          pageContract={pageContract}
          scheduleHref={scheduleHref}
          commandHref={commandHref}
        />
        <Alert
          severity="error"
          action={
            // LivePoller unmounts on a failed read and router.refresh() is this page's only refresh
            // path: without a retry control one transient read failure freezes the board.
            <Button component={Link} href={liveTrackerHref(params)} replace color="inherit" size="small">
              {copy(pageContract, "action.retry")}
            </Button>
          }
        >
          <AlertTitle>{copy(pageContract, "state.error_title")}</AlertTitle>
          {copy(pageContract, "state.error_body")} {result.error.message}
        </Alert>
      </Box>
    );
  }

  const data = result.data;
  const asOfDate = /^\d{4}-\d{2}-\d{2}/.test(params.scope.asOf ?? "") ? params.scope.asOf!.slice(0, 10) : null;
  const resetHref = liveTrackerResetHref(params);
  const filters = buildFilters(params, data, pageContract);
  // hasFilter deliberately excludes the page's park scope, and liveTrackerResetHref re-emits that
  // scope, so offering "clear all" for a bare park selection would navigate to the identical URL.
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
    <Box>
      <PageHead
        pageContract={pageContract}
        scheduleHref={scheduleHref}
        commandHref={commandHref}
      />
      {/* App overview welcome row (TR1-#31, template AppWelcome): the drive day, how many parks are
          running and the live poller (LIVE toggle, updated time, interval) live here instead of as
          header chips; an empty day says so in the welcome text with its reset action, never as a
          separate info banner. The wording turns on hasNarrowing, which INCLUDES the top-bar park. */}
      <Box sx={{ mb: LT_BLOCK_MB }}>
        <AppWelcome
          title={`${copy(pageContract, "page.heading_prefix")} — ${data.business_date ? fmtDriveDay(data.business_date) : copy(pageContract, "label.placeholder")}`}
          description={
            <>
              <Label variant="soft" color={data.is_live_day ? "success" : "default"} sx={{ mb: 1 }}>
                {data.kpis.active_parks}{" "}
                {data.is_live_day
                  ? copy(pageContract, data.kpis.active_parks === 1 ? "chip.parks_running_one" : "chip.parks_running_many")
                  : copy(pageContract, data.kpis.active_parks === 1 ? "chip.parks_active_one" : "chip.parks_active_many")}
              </Label>
              {isEmpty ? (
                <Box component="span" sx={{ display: "block" }}>
                  <Box component="strong" sx={{ display: "block" }}>
                    {params.hasNarrowing ? copy(pageContract, "state.empty_filtered_title") : copy(pageContract, "state.empty_title")}
                  </Box>
                  {params.hasNarrowing ? copy(pageContract, "state.empty_filtered_body") : copy(pageContract, "state.empty_body")}
                </Box>
              ) : null}
              {data.generated_at && data.is_live_day ? (
                <Box component="span" sx={{ display: "block", mt: isEmpty ? 1 : 0 }}>
                  {copy(pageContract, "live.updated_prefix")} <b>{fmtClockSeconds(data.generated_at)}</b> {copy(pageContract, "live.updated_suffix")}
                </Box>
              ) : null}
            </>
          }
          action={
            (data.generated_at && data.is_live_day) || (isEmpty && params.hasFilter) ? (
              <Stack direction="row" spacing={1.5} useFlexGap sx={{ flexWrap: "wrap", alignItems: "center", justifyContent: { xs: "center", md: "flex-start" } }}>
                {data.generated_at && data.is_live_day ? <LivePoller generatedAt={data.generated_at} pageContract={pageContract} /> : null}
                {isEmpty && params.hasFilter ? (
                  <Button component={Link} href={resetHref} replace scroll={false} variant="contained" color="primary">
                    {copy(pageContract, "action.reset_filters")}
                  </Button>
                ) : null}
              </Stack>
            ) : undefined
          }
        />
      </Box>

      <LiveTrackerFilters
        filters={filters}
        clearAllHref={clearAllHref}
        optionsTruncated={data.filter_options.truncated}
        pageContract={pageContract}
        extra={data.generated_at && data.is_live_day ? <LiveIntervalField pageContract={pageContract} /> : undefined}
      />

      {/* The drive-day board (guard: url-keyed-panel): a filter / park change swaps it to its skeleton
          at once; header and filters stay on screen. The poller's refresh is not a navigation. */}
      <UrlSuspense searchParams={params.sp} watch={[ALL_PARAMS]} ignore={DRAWER_PARAMS} fallback={<LiveTrackerBodySkeleton />}>
      {/* The top bar's as_of travels into every nav leaf including this one, but this surface is a
          DRIVE DAY board keyed on business_date — it does not honour as_of, and the sibling
          vaccination reads reject a past as_of outright. Silently answering with a different day
          than the URL claims is the failure mode; saying which day is on screen is the fix. */}
      <Stack spacing={3}>
      {asOfDate && asOfDate !== data.business_date ? (
        <Alert severity="info" role="status">
          {copy(pageContract, "label.as_of_note")}
        </Alert>
      ) : null}

      <LiveTrackerKpis kpis={data.kpis} truncated={data.cells_truncated} pageContract={pageContract} />

      {/* Template overview grid: tables md 8, the live rail md 4. */}
      <Grid container spacing={3}>
        <Grid size={LT_MAIN_SIZE}>
        <Stack spacing={3}>
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
        </Stack>
        </Grid>
        <Grid size={LT_RAIL_SIZE}>
        <LiveTrackerRail
          activity={data.activity}
          scanCaptureTotal={data.kpis.scan_captures}
          proofVideoTotal={data.kpis.proof_videos_received}
          attention={data.attention}
          attentionTotal={data.attention_total}
          attentionTruncated={data.attention_truncated}
          verification={data.verification}
          verifyHref={verifyHref}
          pageContract={pageContract}
        />
        </Grid>
      </Grid>
      </Stack>
      </UrlSuspense>

      <LiveTrackerPassportDrawer
        rows={data.combo.rows}
        initialSelectedGoatId={selectedGoatId}
        closeHref={closePassportHref}
        pageContract={pageContract}
      />
    </Box>
  );
}

function PageHead({
  pageContract,
  scheduleHref,
  commandHref,
}: {
  pageContract: AdminUiPageContract;
  scheduleHref: string;
  commandHref: string;
}) {
  return (
    <Box sx={{ mb: LT_HEADER_MB }}>
    <PageHeader
      title={copy(pageContract, "page.title")}
      crumbs={[{ label: copy(pageContract, "crumb") }, { label: copy(pageContract, "page.title") }]}
      actions={
        <>
        <Button component="a" href={scheduleHref} variant="outlined" color="inherit" startIcon={<Iconify icon="solar:calendar-date-bold" />}>
          {copy(pageContract, "action.full_schedule")}
        </Button>
        <Button component={Link} href={commandHref} variant="contained" color="primary" startIcon={<Iconify icon="solar:monitor-bold" />}>
          {copy(pageContract, "action.command_board")}
        </Button>
        </>
      }
    />
    </Box>
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
      // Park is authoritative for this page's backend read. The shell hides its duplicate selector
      // on this route, so users do not have to change global scope before this filter works.
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

/** The passport drawer opens client-locally; it never changes the board's data. */
const DRAWER_PARAMS = ["goat_passport"] as const;
