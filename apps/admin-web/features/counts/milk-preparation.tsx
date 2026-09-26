import Table from "@mui/material/Table";
import { UrlSuspense } from "@/components/app/url-suspense";
import { PanelSkeleton } from "@/components/app/panel-skeleton";
import { TableSkeleton } from "@/components/app/skeletons";
import { ALL_PARAMS } from "@/components/app/url-tab-nav";
import TableBody from "@mui/material/TableBody";
import TableRow from "@mui/material/TableRow";
import TableCell from "@mui/material/TableCell";
import { listOrEmpty } from "@/lib/list-or-empty";
import { redirect } from "next/navigation";

import Card from "@mui/material/Card";
import CardHeader from "@mui/material/CardHeader";
import Divider from "@mui/material/Divider";
import Grid from "@mui/material/Grid";
import Stack from "@mui/material/Stack";
import AlertTitle from "@mui/material/AlertTitle";
import { EmptyContent } from "@/components/minimal/empty-content";
import { Label } from "@/components/minimal/label";
import { Scrollbar } from "@/components/minimal/scrollbar";
import { TableHeadCustom } from "@/components/minimal/table";
import { InvoiceAnalytic } from "@/components/minimal/sections/invoice/invoice-analytic";
import { EcommerceWidgetSummary } from "@/components/minimal/sections/overview/e-commerce/ecommerce-widget-summary";
import { PageHeader } from "@/components/app/page-header";
import { FeedAnalyticsExport as MilkPreparationExport } from "@/components/analytics-export";
import { WorklistFilters, type WorklistFilterField } from "@/components/worklist-filters";
import { WorklistPager } from "@/components/worklist-pager";
import { copy, tableLabels, tablePageSizes, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import { fmtDate } from "@/lib/format";
import {
  firstAuthRequiredError,
  getMilkPreparation,
  listAnimalStages,
  type MilkPreparationRow,
} from "@/lib/api/server";
import { getCensusLocations } from "@/lib/api/herd-locations";
import { INTERNAL_LOGIN_PATH } from "@/lib/auth/session-cookie";
import { operationalLocationLabel } from "@/lib/operational-location";
import { backendScope, parseScope } from "@/lib/scope";
import { one, type RouteSearchParams } from "@/lib/search-params";
import { stageLabel } from "@/lib/stage-labels";
import Alert from "@mui/material/Alert";

const PAGE_PATH = "/counts/milk-preparation";
const DEFAULT_PAGE_SIZE = 10;

type MilkPreparationSession = MilkPreparationRow["sessions"][number];

function boundedPageValue(raw: string | undefined, fallback: number, allowed: readonly number[]): number {
  const parsed = Number(raw);
  return Number.isInteger(parsed) && allowed.includes(parsed) ? parsed : fallback;
}

function boundedOffset(raw: string | undefined): number {
  const parsed = Number(raw);
  return Number.isInteger(parsed) && parsed >= 0 && parsed <= 5000 ? parsed : 0;
}

function litres(millilitres: number): string {
  return (millilitres / 1000).toLocaleString("en-IN", { maximumFractionDigits: 3 });
}

function hrefWith(searchParams: RouteSearchParams, updates: Record<string, string | null>): string {
  const next = new URLSearchParams();
  for (const [key, value] of Object.entries(searchParams)) {
    if (Array.isArray(value)) value.forEach((item) => next.append(key, item));
    else if (value) next.set(key, value);
  }
  for (const [key, value] of Object.entries(updates)) {
    if (value === null) next.delete(key);
    else next.set(key, value);
  }
  const query = next.toString();
  return query ? `${PAGE_PATH}?${query}` : PAGE_PATH;
}

function sessionCell(row: MilkPreparationRow, sessionNo: number, inactive: string, unit: string): string {
  const session = row.sessions.find((item: MilkPreparationSession) => item.session_no === sessionNo);
  if (!session?.active) return inactive;
  return `${litres(session.required_ml)} ${unit}`;
}

type LabelColor = "default" | "success" | "warning" | "error" | "info";

function verificationTag(row: MilkPreparationRow, pageContract: AdminUiPageContract): { color: LabelColor; label: string; title?: string } {
  if (row.status === "blocked") {
    return { color: "error", label: copy(pageContract, "label.blocked"), title: copy(pageContract, "label.missing_shed") };
  }
  switch (row.verification_status) {
    case "pending_verification":
      return { color: "warning", label: copy(pageContract, "label.pending_verification") };
    case "completed":
      return { color: "success", label: copy(pageContract, "label.verified") };
    case "rework":
      return { color: "error", label: copy(pageContract, "label.rework"), title: row.rework_reason };
    default:
      return { color: "default", label: copy(pageContract, "label.not_submitted") };
  }
}

const NO_SPARK = { categories: [], series: [] };

export async function MilkPreparationPage({
  searchParams,
  pageContract,
}: {
  searchParams?: RouteSearchParams;
  pageContract: AdminUiPageContract;
}) {
  const sp = searchParams ?? {};
  const topBarScope = backendScope(parseScope(sp));
  const localParkID = one(sp, "mp_park") ?? "";
  const effectiveParkID = topBarScope.parkId || localParkID || undefined;
  const pageSizeOptions = tablePageSizes(pageContract, "milk-preparation");
  const limit = boundedPageValue(one(sp, "mp_limit"), DEFAULT_PAGE_SIZE, pageSizeOptions);
  const offset = boundedOffset(one(sp, "mp_offset"));

  const [locations, result, stagesResult] = await Promise.all([
    getCensusLocations(),
    getMilkPreparation({ park_id: effectiveParkID, limit, offset }),
    listAnimalStages(),
  ]);
  if (firstAuthRequiredError(result, stagesResult)) redirect(INTERNAL_LOGIN_PATH);
  // The cohort chip shows the tenant's stage NAME (K2 -> "Milk drinking"), never the stored code;
  // a code the vocabulary lacks (or a failed vocabulary read) keeps its own text.
  const stageNames = new Map(
    stagesResult.ok ? listOrEmpty(stagesResult.data.items).filter((s) => s.name).map((s) => [s.stage_code, s.name] as const) : [],
  );
  const cohortLabel = (code: string) => stageNames.get(code) ?? stageLabel(code);

  const page = result.ok ? result.data : null;
  const rows: MilkPreparationRow[] = page?.items ?? [];
  const summary = page?.summary;
  const cols = tableLabels(pageContract, "milk-preparation");
  const unit = copy(pageContract, "label.litres");
  const inactive = copy(pageContract, "label.inactive_session");
  const filters: WorklistFilterField[] = [{
    kind: "select",
    param: "mp_park",
    label: copy(pageContract, "filter.park_label"),
    value: topBarScope.parkId || localParkID,
    options: locations.parks.map((park) => ({ value: park.id, label: park.name })),
    disabledReason: topBarScope.parkId ? copy(pageContract, "filter.scope_readonly") : undefined,
  }];

  // Farm verification states: template InvoiceAnalytic strip (count + share of the farms).
  const farmTotal = summary
    ? summary.not_submitted_farm_count + summary.pending_verification_farm_count + summary.completed_farm_count + summary.rework_farm_count
    : 0;
  const farmShare = (n: number) => (farmTotal > 0 ? Math.round((n / farmTotal) * 100) : 0);
  const farmStates = summary
    ? [
        { key: "not_submitted", count: summary.not_submitted_farm_count, icon: "solar:clock-circle-bold", color: "text.secondary" },
        { key: "pending_verification", count: summary.pending_verification_farm_count, icon: "solar:bell-bing-bold", color: "warning.main" },
        { key: "verified", count: summary.completed_farm_count, icon: "solar:verified-check-bold", color: "success.main" },
        { key: "rework", count: summary.rework_farm_count, icon: "solar:restart-bold", color: "error.main" },
      ] as const
    : [];

  return (
    <Stack spacing={3} useFlexGap sx={{ minWidth: 0 }}>
      <div>
        <PageHeader
          title={pageContract.title}
          crumbs={[{ label: copy(pageContract, "crumb"), href: "/counts/herd" }, { label: copy(pageContract, "section.preparation.title") }]}
          actions={
            <MilkPreparationExport
              rows={[cols, ...rows.map((row) => [
                row.park_label || copy(pageContract, "label.unassigned_park"),
                row.operational_location_display || row.shed_label || copy(pageContract, "label.unassigned_shed"),
                cohortLabel(row.management_stage),
                row.head_count,
                litres(row.daily_required_ml),
              ])]}
              filename={pageContract.route_id}
              label={copy(pageContract, "action.export")}
            />
          }
        />
      </div>

      {!result.ok ? (
        <Alert severity="error" variant="outlined">
          <AlertTitle>{copy(pageContract, "state.preparation_unavailable")}</AlertTitle>
          {copy(pageContract, "state.try_again")}
        </Alert>
      ) : null}

      {/* KPIs + farm states (guard: url-keyed-panel): a park / page change swaps them to their
          skeleton at once; header and the list toolbar stay on screen. */}
      <UrlSuspense searchParams={sp} watch={[ALL_PARAMS]} fallback={<PanelSkeleton kpis={4} />}>
      <Stack spacing={3} useFlexGap sx={{ minWidth: 0 }}>
      {summary ? (
        <Grid container spacing={3} component="section" aria-label={copy(pageContract, "section.preparation.title")}>
          {[
            { key: "sheds", total: summary.shed_count },
            { key: "kids", total: summary.head_count },
            { key: "milk", total: `${litres(summary.total_required_ml)} ${unit}` },
            { key: "citric", total: `${summary.citric_acid_grams} ${copy(pageContract, "label.grams")}` },
          ].map((kpi) => (
            <Grid key={kpi.key} size={{ xs: 12, sm: 6, md: 3 }}>
              <EcommerceWidgetSummary title={copy(pageContract, `kpi.${kpi.key}.label`)} total={kpi.total} chart={NO_SPARK} sx={{ height: 1 }} />
            </Grid>
          ))}
        </Grid>
      ) : null}

      {summary ? (
        <Card>
          <Scrollbar sx={{ minHeight: 108 }}>
            <Stack direction="row" divider={<Divider orientation="vertical" flexItem sx={{ borderStyle: "dashed" }} />} sx={{ py: 2 }}>
              {farmStates.map((state) => (
                <InvoiceAnalytic
                  key={state.key}
                  title={copy(pageContract, `label.${state.key}`)}
                  total={state.count}
                  value={state.count.toLocaleString("en-IN")}
                  caption={`${farmShare(state.count)}%`}
                  percent={farmShare(state.count)}
                  icon={state.icon}
                  color={state.color}
                />
              ))}
            </Stack>
          </Scrollbar>
        </Card>
      ) : null}
      </Stack>
      </UrlSuspense>

      <Card>
        <CardHeader
          title={copy(pageContract, "section.preparation.title")}
          subheader={
            page
              ? copy(pageContract, "label.prepared_for")
                  .replace("{preparation_date}", fmtDate(page.preparation_date))
                  .replace("{feeding_date}", fmtDate(page.feeding_date))
              : undefined
          }
          sx={{ mb: 1 }}
        />
        <WorklistFilters basePath={PAGE_PATH} pageParam="mp_offset" fields={filters} pageContract={pageContract} />

        {/* The rows + pager swap to their skeleton inside the card; the toolbar above stays. */}
        <UrlSuspense searchParams={sp} watch={[ALL_PARAMS]} fallback={<TableSkeleton columns={cols.length} rows={limit} header={false} bare />}>

        <Scrollbar tabIndex={0} role="group" aria-label={copy(pageContract, "section.preparation.aria")}>
          <Table sx={{ minWidth: 800 }} aria-label={copy(pageContract, "table.preparation.aria")}>
            <TableHeadCustom headCells={cols.map((col, i) => ({ id: `${i}`, label: col, sortable: false }))} />
            <TableBody>
              {rows.length === 0 ? (
                <TableRow>
                  <TableCell colSpan={cols.length}>
                    <EmptyContent filled title={copy(pageContract, "empty.preparation")} sx={{ py: 8 }} />
                  </TableCell>
                </TableRow>
              ) : rows.map((row) => {
                const status = verificationTag(row, pageContract);
                const locationLabel = row.operational_location_display || operationalLocationLabel({ shedName: row.shed_label, partitionLabel: row.partition_label });
                return <TableRow hover key={`${row.park_id}|${row.shed_id}|${row.partition_label ?? ""}|${row.management_stage}`}>
                  <TableCell>{row.park_label || copy(pageContract, "label.unassigned_park")}</TableCell>
                  <TableCell sx={{ typography: "subtitle2" }}>{locationLabel || copy(pageContract, "label.unassigned_shed")}</TableCell>
                  <TableCell><Label variant="soft" color="info">{cohortLabel(row.management_stage)}</Label></TableCell>
                  <TableCell align="right">{row.head_count}</TableCell>
                  {[1, 2, 3, 4].map((sessionNo) => <TableCell key={sessionNo} sx={{ color: "text.secondary" }}>{sessionCell(row, sessionNo, inactive, unit)}</TableCell>)}
                  <TableCell align="right" sx={{ typography: "subtitle2" }}>{litres(row.daily_required_ml)} {unit}</TableCell>
                  <TableCell>
                    <Label variant="soft" color={status.color} title={status.title || undefined}>
                      {status.label}
                    </Label>
                  </TableCell>
                </TableRow>
              })}
            </TableBody>
          </Table>
        </Scrollbar>

        <WorklistPager
          pageContract={pageContract}
          offset={offset}
          limit={limit}
          rowCount={rows.length}
          hasMore={page?.has_more ?? false}
          noun={copy(pageContract, "table.preparation.noun")}
          pageSizeOptions={pageSizeOptions}
          hrefForOffset={(nextOffset) => hrefWith(sp, { mp_offset: String(nextOffset) })}
          hrefForLimit={(nextLimit) => hrefWith(sp, { mp_limit: String(nextLimit), mp_offset: null })}
        />
        </UrlSuspense>
      </Card>
    </Stack>
  );
}
