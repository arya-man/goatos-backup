import { randomUUID } from "node:crypto";
import Link from "@/components/no-prefetch-link";
import { LocalOverlayLink } from "@/components/local-overlay-link";
import { redirect } from "next/navigation";
import { ChevronLeft, ChevronRight } from "lucide-react";

import { Tag } from "@/components/ui-primitives";
import { dash } from "@/lib/format";
import { actionFeedbackCopy, copy, tableLabels, tablePageSizes, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import {
  firstAuthRequiredError,
  getCountsBreakdown,
  getHerdRegisterSummary,
  listAnimalStages,
  searchGoats,
  type GoatSearchResponse,
  type HerdRegisterSummaryResponse,
} from "@/lib/api/server";
import { getHerdRegisterLocations } from "@/lib/api/herd-locations";
import { INTERNAL_LOGIN_PATH } from "@/lib/auth/session-cookie";
import { backendScope, parseScope } from "@/lib/scope";
import {
  boundedInt,
  hrefPreviousCursor,
  hrefWithCursor,
  hrefWithoutAction,
  one,
  type RouteSearchParams,
} from "@/lib/search-params";
import { HerdActions, type HerdAnimalStageOption, type HerdOperationalLocationOption } from "./herd-actions-ui";
import { HerdFiltersModalClient } from "./herd-filters-modal-client";
import { HerdPassportLocalDrawer, type HerdPassportDrawerItem } from "./herd-passport-local-drawer";
import { operationalLocationLabel } from "@/lib/operational-location";

// Counts -> Herd Register. The vaccination cascade's real business entry point: register/import a goat,
// emit goat.created, generate vaccination obligations. This screen is the OPERATIONAL Counts module surface.
// KPI summary cards paginate through all /goats/search rows in scope (100 per page) for exact totals.
// Kid vs adult uses goat age_band and management_stage from the API (K1/K2/... stages), not shed names.
//
// Generated-client status: goat READ and WRITE operation IDs are present and wired. The herd table +
// filters read /goats/search; Register goat and Import sheet open real drawers that post createAdminGoat /
// bulk preview+commit (see herd-actions.ts / herd-actions-ui.tsx). No hand-rolled DTOs, no fake rows.
// New report has no API and stays disabled.

const DEFAULT_PAGE_SIZE = 10;

type GoatRow = GoatSearchResponse["items"][number];
type HerdSummaryRow = HerdRegisterSummaryResponse["items"][number] & Record<string, unknown>;

function summaryNumber(row: HerdSummaryRow, camelKey: string, snakeKey: string): number {
  const value = row[camelKey] ?? row[snakeKey];
  return typeof value === "number" && Number.isFinite(value) ? value : 0;
}

function hrefWithDrawerParam(pathname: string, params: RouteSearchParams, key: string, value: string | null): string {
  const next = new URLSearchParams();
  for (const [paramKey, paramValue] of Object.entries(params)) {
    if (paramKey === key) continue;
    if (Array.isArray(paramValue)) {
      for (const item of paramValue) if (item) next.append(paramKey, item);
    } else if (paramValue) {
      next.set(paramKey, paramValue);
    }
  }
  if (value) next.set(key, value);
  const qs = next.toString();
  return qs ? `${pathname}?${qs}` : pathname;
}

function statusTone(value: string | null | undefined, kind: "lifecycle" | "health" | "breeding"): "ok" | "warn" | "dng" | "info" | "mut" {
  const v = String(value ?? "").toLowerCase();
  if (!v) return "mut";
  if (kind === "health") {
    if (["healthy", "normal", "ok"].includes(v)) return "ok";
    if (["sick", "critical", "dead"].includes(v)) return "dng";
    if (v.includes("treatment") || v.includes("watch") || v.includes("quarantine")) return "warn";
    return "info";
  }
  if (kind === "breeding") {
    if (v.includes("pregnant") || v.includes("lactating") || v.includes("ai")) return "info";
    if (v.includes("open") || v.includes("none")) return "mut";
    return "ok";
  }
  if (["alive", "active"].includes(v)) return "ok";
  if (["sold", "died", "culled", "lost", "inactive"].includes(v)) return "dng";
  return "mut";
}

function locationLabel(g: GoatRow, part: "park" | "shed"): string {
  const path = g.location_path;
  if (part === "park") return path.park_code ?? path.park_name ?? "—";
  const shedName = path.shed_name ?? path.shed_code ?? "";
  if (!shedName) return "—";
  return (
    path.operational_location_display ||
    operationalLocationLabel({
      shedName,
      partitionLabel: path.partition_label,
      sourceShedName: path.source_shed_name,
    })
  );
}

function weightLabel(weight: number | null | undefined): string {
  return typeof weight === "number" && Number.isFinite(weight) ? `${weight.toFixed(weight % 1 === 0 ? 0 : 1)}` : "—";
}

// KPIs come from canonical scoped goat counts. `summary === null` means the
// API read failed: show an honest dash, never fabricate a fallback.
function buildHerdSummary(pageContract: AdminUiPageContract, summary: HerdRegisterSummaryResponse | null) {
  const totals = summary
    ? summary.items.reduce(
        (acc, row) => ({
          total: acc.total + summaryNumber(row, "totalCount", "total_count"),
          active: acc.active + summaryNumber(row, "activeCount", "active_count"),
          adult: acc.adult + summaryNumber(row, "adultCount", "adult_count"),
          kid: acc.kid + summaryNumber(row, "kidCount", "kid_count"),
          untaggedKid: acc.untaggedKid + summaryNumber(row, "untaggedKidCount", "untagged_kid_count"),
          dead: acc.dead + summaryNumber(row, "deadCount", "dead_count"),
          sold: acc.sold + summaryNumber(row, "soldCount", "sold_count"),
          culled: acc.culled + summaryNumber(row, "culledCount", "culled_count"),
        }),
        { total: 0, active: 0, adult: 0, kid: 0, untaggedKid: 0, dead: 0, sold: 0, culled: 0 },
      )
    : null;
  const fmt = (value: number | undefined) => (totals ? `${value}` : dash(null));
  const unavailable = copy(pageContract, "section.summary.unavailable");
  const activeSub = totals ? `${totals.active} ${copy(pageContract, "label.live_rows")}` : unavailable;
  return [
    { label: copy(pageContract, "label.total_records"), value: fmt(totals?.total), sub: totals ? copy(pageContract, "label.seeded_goat_rows") : unavailable },
    { label: copy(pageContract, "label.active"), value: fmt(totals?.active), sub: activeSub },
    { label: copy(pageContract, "label.adults"), value: fmt(totals?.adult), sub: totals ? copy(pageContract, "label.live_scoped_register") : unavailable },
    { label: copy(pageContract, "label.kids"), value: fmt(totals?.kid), sub: totals ? copy(pageContract, "label.stage_shed_inferred") : unavailable },
    { label: copy(pageContract, "label.untagged_kids"), value: fmt(totals?.untaggedKid), sub: totals ? copy(pageContract, "label.identity") : unavailable },
    { label: copy(pageContract, "label.dead"), value: fmt(totals?.dead), sub: totals ? copy(pageContract, "label.terminal_rows") : unavailable },
    { label: copy(pageContract, "label.sold"), value: fmt(totals?.sold), sub: totals ? copy(pageContract, "label.terminal_rows") : unavailable },
    { label: copy(pageContract, "label.culled"), value: fmt(totals?.culled), sub: totals ? copy(pageContract, "label.terminal_rows") : unavailable },
  ];
}

export async function HerdRegisterPage({
  searchParams,
  pageContract,
}: {
  searchParams?: RouteSearchParams;
  pageContract: AdminUiPageContract;
}) {
  const sp = searchParams ?? {};
  const pathname = "/counts/herd";

  // Top bar owns park/as-of scope. parseScope is the single sanctioned reader; backendScope maps it to the
  // params the API actually honors. searchGoats honors park_id; it has no as_of param, so as_of is preserved
  // in the URL for the top bar but not sent here.
  const scope = parseScope(sp);
  const { parkId } = backendScope(scope);

  const q = one(sp, "q");
  const breed = one(sp, "breed");
  const sex = one(sp, "sex");
  const status = "alive";
  const pageSizeOptions = tablePageSizes(pageContract, "herd-register");
  const requestedLimit = Number(one(sp, "limit"));
  const pageSize = pageSizeOptions.includes(requestedLimit) ? requestedLimit : DEFAULT_PAGE_SIZE;
  const cursor = one(sp, "cursor");
  const page = boundedInt(one(sp, "page"), 1, 1, 1_000_000);
  const hasFilter = Boolean(q || breed || sex);
  const actionStatus = one(sp, "action_status");
  const actionKey = one(sp, "action_key");
  const returnTo = hrefWithoutAction(pathname, sp);
  const selectedGoatId = one(sp, "goat_passport");

  // Real goats + real location options for the write drawers, in parallel.
  const [result, summaryResult, locations, stagesResult, breakdownResult] = await Promise.all([
    searchGoats({ limit: pageSize, cursor, q, breed, sex, park_id: parkId, status }),
    getHerdRegisterSummary({ park_id: parkId, breed, sex }),
    getHerdRegisterLocations(),
    listAnimalStages(),
    getCountsBreakdown({ lifecycle_status: status, limit: 1 }),
  ]);
  const authError = firstAuthRequiredError(result, summaryResult, stagesResult, breakdownResult);
  if (authError) redirect(INTERNAL_LOGIN_PATH);

  // A fresh idempotency key per render: a double-submit of the open Register drawer replays the same key
  // (backend returns the original goat); a reload mints a new key for a new logical create.
  const registerIdempotencyKey = randomUUID();
  // Dedicated key for the reproductive edit drawer so it never shares/replays the register create key.
  const reproductiveIdempotencyKey = randomUUID();
  const animalStages: HerdAnimalStageOption[] = stagesResult.ok
    ? stagesResult.data.items.map((stage) => ({
        code: stage.stage_code,
        label: stage.name ? `${stage.stage_code} · ${stage.name}` : stage.stage_code,
      }))
    : [];
  const operationalLocations: HerdOperationalLocationOption[] = breakdownResult.ok
    ? breakdownResult.data.facets.sheds.map((shed) => ({
        key: shed.key,
        shedId: shed.shed_id,
        parkId: shed.park_id || null,
        partitionLabel: shed.partition_label || null,
        label: shed.operational_location_display || shed.label,
      }))
    : [];

  const goats: GoatRow[] = result.ok ? result.data.items : [];
  // Honest state: an unavailable summary read shows a dash, not fabricated numbers.
  const summaryCards = buildHerdSummary(pageContract, summaryResult.ok ? summaryResult.data : null);
  const nextCursor = result.ok ? result.data.next_cursor ?? null : null;
  const nextHref = hrefWithCursor(pathname, sp, nextCursor);
  const prevHref = hrefPreviousCursor(pathname, sp);
  const scopedPark = parkId ? locations.parks.find((p) => p.id === parkId) : null;
  const herdContext = scopedPark ? `${scopedPark.code ?? scopedPark.name} · ${copy(pageContract, "label.all_sheds")}` : copy(pageContract, "label.all_parks");
  const cols = tableLabels(pageContract, "herd-register");
  const closePassportHref = hrefWithDrawerParam(pathname, sp, "goat_passport", null);
  const drawerItems: HerdPassportDrawerItem[] = goats.map((goat) => ({
    goatId: goat.goat_id,
    displayId: goat.display_id,
    tag1: goat.animal_identifier_1,
    tag2: goat.animal_identifier_2,
    park: locationLabel(goat, "park"),
    shed: locationLabel(goat, "shed"),
    breed: goat.breed,
    sex: goat.sex,
    weightKg: goat.weight_kg,
    lifecycleStatus: goat.lifecycle_status,
    healthStatus: goat.health_status,
    reproductiveStatus: goat.reproductive_status,
  }));

  return (
    <div className="screen on">
      <div className="phead">
        <div>
	          <div className="crumb">
	            {copy(pageContract, "crumb")} / <b>{copy(pageContract, "section.herd.title")}</b>
	          </div>
	          <h1>{pageContract.title}</h1>
	        </div>
        <div className="sp" style={{ flex: 1 }} />
        {/* Import sheet + Register goat open real drawers wired to the generated admin goat clients. */}
        <HerdActions
          parks={locations.parks}
          sheds={locations.sheds}
          operationalLocations={operationalLocations}
          farms={locations.farms}
          animalStages={animalStages}
          locationsAvailable={locations.available}
          stagesAvailable={stagesResult.ok}
          idempotencyKey={registerIdempotencyKey}
          returnTo={returnTo}
          pageContract={pageContract}
        />
        <button
          type="button"
          className="btn"
          disabled
          aria-disabled="true"
	          title={copy(pageContract, "reason.report_pending")}
          style={{ opacity: 0.5, cursor: "not-allowed" }}
        >
	          {copy(pageContract, "action.new_report")}
        </button>
      </div>

      {actionStatus ? (
        actionStatus === "success" ? (
          <div className="note" style={{ marginBottom: 12 }}>
	            <Tag tone="ok">{copy(pageContract, "action.success_tag")}</Tag> {actionFeedbackCopy(pageContract, actionStatus, actionKey)}
          </div>
        ) : (
          <div className="alert" style={{ marginBottom: 12 }}>
	            <b>{copy(pageContract, "action.failed_title")}</b>&nbsp;{actionFeedbackCopy(pageContract, actionStatus, actionKey)}
          </div>
        )
      ) : null}

      <div className="grid herd-kpi-grid" style={{ marginBottom: 8 }}>
        {summaryCards.map((card) => (
          <div key={card.label} className="kpi" title={copy(pageContract, "section.summary.tooltip")}>
            <div className="lab">{card.label}</div>
            <div className="val">{card.value}</div>
            <div className="dl">
              <span className="muted">{card.sub}</span>
            </div>
          </div>
        ))}
      </div>
      <div className="note" style={{ marginBottom: 16 }}>
	        {copy(pageContract, "section.summary.note")}
      </div>

      {!result.ok ? (
        <div className="alert" style={{ marginBottom: 16 }}>
          <b>{result.error.code ?? result.error.kind}</b>&nbsp;{result.error.message}
        </div>
      ) : null}

      <section className="card">
        <div className="hd">
	          <h3>{copy(pageContract, "section.herd.title")}</h3>
	          <span className="small muted">{herdContext}</span>
	          <div className="sp" style={{ flex: 1 }} />
	          <span className="muted small">{copy(pageContract, "section.herd.row_hint")}</span>
        </div>
        <HerdFiltersModalClient rowCount={goats.length} pageSize={pageSize} pageSizeOptions={pageSizeOptions} hasFilters={hasFilter} pageContract={pageContract} />
	        <div className="bd" style={{ padding: 0, overflowX: "auto" }} tabIndex={0} role="group" aria-label={copy(pageContract, "section.herd.aria")}>
          <table className="herd-register-table">
            <thead>
              <tr>
	                {cols.map((c) => (
	                  <th key={c}>{c}</th>
                ))}
              </tr>
            </thead>
            <tbody>
              {goats.length === 0 ? (
                <tr>
	                  <td colSpan={cols.length}>
                    <div className="muted small" style={{ padding: "18px 4px", textAlign: "center", lineHeight: 1.6 }}>
	                      {result.ok
	                        ? hasFilter
	                          ? copy(pageContract, "empty.herd_filtered")
	                          : copy(pageContract, "empty.herd")
	                        : copy(pageContract, "empty.unavailable")}
                    </div>
                  </td>
                </tr>
              ) : (
                goats.map((g) => {
                  const href = hrefWithDrawerParam(pathname, sp, "goat_passport", g.goat_id);
                  return (
                    <tr key={g.goat_id}>
                      <td>
                        <LocalOverlayLink href={href} className="celllink" scroll={false}>
                          <span className="gid">{g.display_id}</span>
                        </LocalOverlayLink>
                      </td>
                      <td>
                        <LocalOverlayLink href={href} className="celllink mono" scroll={false}>{dash(g.animal_identifier_1)}</LocalOverlayLink>
                      </td>
                      <td>
                        <LocalOverlayLink href={href} className="celllink mono" scroll={false}>{dash(g.animal_identifier_2)}</LocalOverlayLink>
                      </td>
                      <td className="muted">
                        <LocalOverlayLink href={href} className="celllink" scroll={false}>{locationLabel(g, "park")}</LocalOverlayLink>
                      </td>
                      <td className="muted">
                        <LocalOverlayLink href={href} className="celllink" scroll={false}>{locationLabel(g, "shed")}</LocalOverlayLink>
                      </td>
                      <td>
                        <LocalOverlayLink href={href} className="celllink" scroll={false}>{dash(g.breed)}</LocalOverlayLink>
                      </td>
                      <td>
                        <LocalOverlayLink href={href} className="celllink" scroll={false}>{dash(g.sex)}</LocalOverlayLink>
                      </td>
                      <td className="muted">
                        <LocalOverlayLink href={href} className="celllink" scroll={false}>
                          {weightLabel(g.weight_kg)}{g.weight_kg ? <span className="muted small"> kg</span> : null}
                        </LocalOverlayLink>
                      </td>
                      <td>
                        <LocalOverlayLink href={href} className="celllink" scroll={false}>
                          <Tag tone={statusTone(g.lifecycle_status, "lifecycle")}>{dash(g.lifecycle_status)}</Tag>
                        </LocalOverlayLink>
                      </td>
                      <td>
                        <LocalOverlayLink href={href} className="celllink" scroll={false}>
                          <Tag tone={statusTone(g.health_status, "health")}>{dash(g.health_status)}</Tag>
                        </LocalOverlayLink>
                      </td>
                      <td>
                        <LocalOverlayLink href={href} className="celllink" scroll={false}>
                          <Tag tone={statusTone(g.reproductive_status, "breeding")}>{dash(g.reproductive_status)}</Tag>
                        </LocalOverlayLink>
                      </td>
                    </tr>
                  );
                })
              )}
            </tbody>
          </table>
        </div>
        {prevHref || nextHref || page > 1 ? (
	          <div className="pager2">
	            <span className="muted small">
	              {copy(pageContract, "pager.page")} {page} · {goats.length} {copy(pageContract, goats.length === 1 ? "label.row_singular" : "label.row_plural")}
	              {nextCursor ? ` · ${copy(pageContract, "pager.scale_note")}` : ` · ${copy(pageContract, "pager.end_note")}`}
            </span>
            {prevHref ? (
              <Link href={prevHref} scroll={false} className="btn sm">
	                <ChevronLeft className="ic" style={{ width: 13 }} aria-hidden="true" /> {copy(pageContract, "action.previous")}
              </Link>
            ) : (
              <span className="btn sm" aria-disabled="true" style={{ opacity: 0.45, cursor: "not-allowed" }}>
	                <ChevronLeft className="ic" style={{ width: 13 }} aria-hidden="true" /> {copy(pageContract, "action.previous")}
              </span>
            )}
            {nextHref ? (
              <Link href={nextHref} scroll={false} className="btn sm">
	                {copy(pageContract, "action.next")} <ChevronRight className="ic" style={{ width: 13 }} aria-hidden="true" />
              </Link>
            ) : (
              <span className="btn sm" aria-disabled="true" style={{ opacity: 0.45, cursor: "not-allowed" }}>
	                {copy(pageContract, "action.next")} <ChevronRight className="ic" style={{ width: 13 }} aria-hidden="true" />
              </span>
            )}
          </div>
        ) : null}
      </section>
      <HerdPassportLocalDrawer
        items={drawerItems}
        initialSelectedId={selectedGoatId}
        closeHref={closePassportHref}
        reproductiveIdempotencyKey={reproductiveIdempotencyKey}
        returnTo={returnTo}
        pageContract={pageContract}
      />
    </div>
  );
}
