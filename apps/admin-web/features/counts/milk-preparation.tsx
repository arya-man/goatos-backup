import Table from "@mui/material/Table";
import { UrlSuspense } from "@/components/app/url-suspense";
import { PanelSkeleton } from "@/components/app/panel-skeleton";
import { ALL_PARAMS } from "@/components/app/url-tab-nav";
import TableHead from "@mui/material/TableHead";
import TableBody from "@mui/material/TableBody";
import TableRow from "@mui/material/TableRow";
import TableCell from "@mui/material/TableCell";
import { listOrEmpty } from "@/lib/list-or-empty";
import { redirect } from "next/navigation";
import { Baby, Beaker, Fence, Milk } from "lucide-react";

import Card from "@mui/material/Card";
import CardHeader, { cardHeaderClasses } from "@mui/material/CardHeader";
import { PageHeader } from "@/components/app/page-header";
import { KpiCard, KpiGrid } from "@/components/minimal/widgets";
import { Tag } from "@/components/ui-primitives";
import "./milk-preparation.css";
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

function verificationTag(row: MilkPreparationRow, pageContract: AdminUiPageContract) {
  if (row.status === "blocked") {
    return { className: "tag t-dng", label: copy(pageContract, "label.blocked"), title: copy(pageContract, "label.missing_shed") };
  }
  switch (row.verification_status) {
    case "pending_verification":
      return { className: "tag t-warn", label: copy(pageContract, "label.pending_verification") };
    case "completed":
      return { className: "tag t-ok", label: copy(pageContract, "label.verified") };
    case "rework":
      return { className: "tag t-dng", label: copy(pageContract, "label.rework"), title: row.rework_reason };
    default:
      return { className: "tag t-mut", label: copy(pageContract, "label.not_submitted") };
  }
}

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

  return (
    <div className="kit-enter screen on">
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
        <Alert severity="error" style={{ marginBottom: 16 }}><div>
            <b>{copy(pageContract, "state.preparation_unavailable")}</b>
            <div className="small muted">{copy(pageContract, "state.try_again")}</div>
          </div>
        </Alert>
      ) : null}

      <div>
        <WorklistFilters basePath={PAGE_PATH} pageParam="mp_offset" fields={filters} pageContract={pageContract} />
      </div>

      {/* KPIs + table (guard: url-keyed-panel): a filter / page change swaps them to their skeleton at
          once; header and filters stay on screen. */}
      <UrlSuspense searchParams={sp} watch={[ALL_PARAMS]} fallback={<PanelSkeleton kpis={4} table={limit} tableWidths={cols.map(() => "1fr")} />}>
      {summary ? (
        <div>
          <KpiGrid min={210} className="milk-prep-kpis">
            <KpiCard tone="primary" icon={<Fence size={22} />} label={copy(pageContract, "kpi.sheds.label")} value={summary.shed_count} />
            <KpiCard tone="info" icon={<Baby size={22} />} label={copy(pageContract, "kpi.kids.label")} value={summary.head_count} />
            <KpiCard tone="success" icon={<Milk size={22} />} label={copy(pageContract, "kpi.milk.label")} value={`${litres(summary.total_required_ml)} ${unit}`} />
            <KpiCard tone="violet" icon={<Beaker size={22} />} label={copy(pageContract, "kpi.citric.label")} value={`${summary.citric_acid_grams} ${copy(pageContract, "label.grams")}`} />
          </KpiGrid>
        </div>
      ) : null}

      <div>
      <Card className="kit-tablecard milk-prep-card">
        <CardHeader
          className="milk-prep-head"
          sx={{ flexWrap: "wrap", rowGap: 1.5, [`& .${cardHeaderClasses.action}`]: { m: 0, flex: { xs: "1 1 100%", sm: "0 0 auto" }, minWidth: 0, maxWidth: "100%" } }}
          title={copy(pageContract, "section.preparation.title")}
          action={
            <span className="milk-prep-chips">
              {page ? (
                <Tag tone="info">
                  {copy(pageContract, "label.prepared_for")
                    .replace("{preparation_date}", fmtDate(page.preparation_date))
                    .replace("{feeding_date}", fmtDate(page.feeding_date))}
                </Tag>
              ) : null}
              {summary ? (
                <>
                  <Tag tone="mut">{copy(pageContract, "label.not_submitted")}: {summary.not_submitted_farm_count}</Tag>
                  <Tag tone="warn">{copy(pageContract, "label.pending_verification")}: {summary.pending_verification_farm_count}</Tag>
                  <Tag tone="ok">{copy(pageContract, "label.verified")}: {summary.completed_farm_count}</Tag>
                  <Tag tone="dng">{copy(pageContract, "label.rework")}: {summary.rework_farm_count}</Tag>
                </>
              ) : null}
            </span>
          }
        />

        <div className="tablewrap feed-scroll" tabIndex={0} role="group" aria-label={copy(pageContract, "section.preparation.aria")}>
          <Table className="feed-table" aria-label={copy(pageContract, "table.preparation.aria")}>
            <TableHead><TableRow>{cols.map((col) => <TableCell component="th" key={col}>{col}</TableCell>)}</TableRow></TableHead>
            <TableBody>
              {rows.length === 0 ? (
                <TableRow><TableCell colSpan={cols.length}><div className="muted small" style={{ padding: "18px 4px", textAlign: "center" }}>{copy(pageContract, "empty.preparation")}</div></TableCell></TableRow>
              ) : rows.map((row) => {
                const status = verificationTag(row, pageContract);
                const locationLabel = row.operational_location_display || operationalLocationLabel({ shedName: row.shed_label, partitionLabel: row.partition_label });
                return <TableRow key={`${row.park_id}|${row.shed_id}|${row.partition_label ?? ""}|${row.management_stage}`}>
                  <TableCell>{row.park_label || copy(pageContract, "label.unassigned_park")}</TableCell>
                  <TableCell>{locationLabel || copy(pageContract, "label.unassigned_shed")}</TableCell>
                  <TableCell><Tag tone="info">{cohortLabel(row.management_stage)}</Tag></TableCell>
                  <TableCell className="num">{row.head_count}</TableCell>
                  {[1, 2, 3, 4].map((sessionNo) => <TableCell key={sessionNo}>{sessionCell(row, sessionNo, inactive, unit)}</TableCell>)}
                  <TableCell className="num" style={{ fontWeight: 650 }}>{litres(row.daily_required_ml)} {unit}</TableCell>
                  <TableCell>
                    <span className={status.className} title={status.title || undefined}>
                      {status.label}
                    </span>
                  </TableCell>
                </TableRow>
              })}
            </TableBody>
          </Table>
        </div>

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
      </Card>
      </div>
      </UrlSuspense>
    </div>
  );
}
