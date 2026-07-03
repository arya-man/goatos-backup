import { randomUUID } from "node:crypto";
import Link from "next/link";
import { redirect } from "next/navigation";
import { ChevronLeft, ChevronRight, X } from "lucide-react";

import { Tag } from "@/components/ui-primitives";
import { dash } from "@/lib/format";
import { actionFeedbackCopy, copy, tableLabels, tablePageSizes, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import {
  firstAuthRequiredError,
  listAnimalStages,
  searchGoats,
  type GoatSearchResponse,
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
import { HerdActions, type HerdAnimalStageOption } from "./herd-actions-ui";
import { HerdFiltersModalClient } from "./herd-filters-modal-client";
import { HerdPassportVaccinationBlock } from "./herd-passport-vaccination";

// Counts -> Herd Register. The vaccination cascade's real business entry point: register/import a goat,
// emit goat.created, generate vaccination obligations. This screen is the OPERATIONAL Counts module surface.
//
// Generated-client status: goat READ and WRITE operation IDs are present and wired. The herd table +
// filters read /goats/search; Register goat and Import sheet open real drawers that post createAdminGoat /
// bulk preview+commit (see herd-actions.ts / herd-actions-ui.tsx). No hand-rolled DTOs, no fake rows.
// New report has no API and stays disabled.

const DEFAULT_PAGE_SIZE = 10;
const SUMMARY_LIMIT = 100;

type GoatRow = GoatSearchResponse["items"][number];

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
  return path.shed_name ?? path.shed_code ?? "—";
}

function weightLabel(weight: number | null | undefined): string {
  return typeof weight === "number" && Number.isFinite(weight) ? `${weight.toFixed(weight % 1 === 0 ? 0 : 1)}` : "—";
}

function isActiveGoat(g: GoatRow): boolean {
  const status = String(g.lifecycle_status ?? "").toLowerCase();
  return status === "alive" || status === "active";
}

function isKidGoat(g: GoatRow): boolean {
  const text = [
    g.location_path.shed_name,
    g.location_path.shed_code,
    g.lifecycle_status,
    g.reproductive_status,
  ]
    .filter(Boolean)
    .join(" ")
    .toLowerCase();
  return /\b(k\d|kid|kids|weaner|nursery)\b/.test(text);
}

function isUntagged(g: GoatRow): boolean {
  return !g.animal_identifier_1 || !g.animal_identifier_2;
}

function buildHerdSummary(pageContract: AdminUiPageContract, rows: GoatRow[], capped: boolean) {
  const activeRows = rows.filter(isActiveGoat);
  const kidRows = activeRows.filter(isKidGoat);
  const adultRows = activeRows.filter((g) => !isKidGoat(g));
  const untaggedKids = kidRows.filter(isUntagged).length;
  const suffix = capped ? "+" : "";
  const sub = capped
    ? `${copy(pageContract, "label.first_live_rows_prefix")} ${rows.length} ${copy(pageContract, "label.live_rows")}`
    : `${rows.length} ${copy(pageContract, "label.live_rows")}`;
  return [
    { label: copy(pageContract, "label.active"), value: `${activeRows.length}${suffix}`, sub },
    { label: copy(pageContract, "label.adults"), value: `${adultRows.length}${suffix}`, sub: copy(pageContract, "label.live_scoped_register") },
    { label: copy(pageContract, "label.kids"), value: `${kidRows.length}${suffix}`, sub: copy(pageContract, "label.stage_shed_inferred") },
    { label: copy(pageContract, "label.untagged_kids"), value: `${untaggedKids}${suffix}`, sub: copy(pageContract, "label.invalid_id_rows") },
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
  const [result, summaryResult, locations, stagesResult] = await Promise.all([
    searchGoats({ limit: pageSize, cursor, q, breed, sex, park_id: parkId }),
    searchGoats({ limit: SUMMARY_LIMIT, q, breed, sex, park_id: parkId }),
    getHerdRegisterLocations(),
    listAnimalStages(),
  ]);
  const authError = firstAuthRequiredError(result, summaryResult, stagesResult);
  if (authError) redirect(INTERNAL_LOGIN_PATH);

  // A fresh idempotency key per render: a double-submit of the open Register drawer replays the same key
  // (backend returns the original goat); a reload mints a new key for a new logical create.
  const registerIdempotencyKey = randomUUID();
  const animalStages: HerdAnimalStageOption[] = stagesResult.ok
    ? stagesResult.data.items.map((stage) => ({
        code: stage.stage_code,
        label: stage.name ? `${stage.stage_code} · ${stage.name}` : stage.stage_code,
      }))
    : [];

  const goats: GoatRow[] = result.ok ? result.data.items : [];
  const summaryRows: GoatRow[] = summaryResult.ok ? summaryResult.data.items : goats;
  const summaryCards = buildHerdSummary(pageContract, summaryRows, Boolean(summaryResult.ok && summaryResult.data.next_cursor));
  const nextCursor = result.ok ? result.data.next_cursor ?? null : null;
  const nextHref = hrefWithCursor(pathname, sp, nextCursor);
  const prevHref = hrefPreviousCursor(pathname, sp);
  const scopedPark = parkId ? locations.parks.find((p) => p.id === parkId) : null;
  const herdContext = scopedPark ? `${scopedPark.code ?? scopedPark.name} · ${copy(pageContract, "label.all_sheds")}` : copy(pageContract, "label.all_parks");
  const cols = tableLabels(pageContract, "herd-register");
  const selectedGoat = selectedGoatId ? goats.find((g) => g.goat_id === selectedGoatId) : undefined;
  const closePassportHref = hrefWithDrawerParam(pathname, sp, "goat_passport", null);

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

      <div className="grid g4" style={{ marginBottom: 8 }}>
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
          <table>
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
                        <Link href={href} className="celllink" scroll={false}>
                          <span className="gid">{g.display_id}</span>
                          {isUntagged(g) ? (
                            <>
                              {" "}
                              <Tag tone="warn" title={copy(pageContract, "tag.identity_title")}>
                                missing ID
                              </Tag>
                            </>
                          ) : null}
                        </Link>
                      </td>
                      <td className="muted">
                        <Link href={href} className="celllink" scroll={false}>{locationLabel(g, "park")}</Link>
                      </td>
                      <td className="muted">
                        <Link href={href} className="celllink" scroll={false}>{locationLabel(g, "shed")}</Link>
                      </td>
                      <td>
                        <Link href={href} className="celllink" scroll={false}>{dash(g.breed)}</Link>
                      </td>
                      <td>
                        <Link href={href} className="celllink" scroll={false}>{dash(g.sex)}</Link>
                      </td>
                      <td className="muted">
                        <Link href={href} className="celllink" scroll={false}>
                          {weightLabel(g.weight_kg)}{g.weight_kg ? <span className="muted small"> kg</span> : null}
                        </Link>
                      </td>
                      <td>
                        <Link href={href} className="celllink" scroll={false}>
                          <Tag tone={statusTone(g.lifecycle_status, "lifecycle")}>{dash(g.lifecycle_status)}</Tag>
                        </Link>
                      </td>
                      <td>
                        <Link href={href} className="celllink" scroll={false}>
                          <Tag tone={statusTone(g.health_status, "health")}>{dash(g.health_status)}</Tag>
                        </Link>
                      </td>
                      <td>
                        <Link href={href} className="celllink" scroll={false}>
                          <Tag tone={statusTone(g.reproductive_status, "breeding")}>{dash(g.reproductive_status)}</Tag>
                        </Link>
                      </td>
                    </tr>
                  );
                })
              )}
            </tbody>
          </table>
        </div>
        {goats.length > 0 || page > 1 ? (
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
      {selectedGoat ? (
        <HerdPassportDrawer
          goat={selectedGoat}
          closeHref={closePassportHref}
	          fullPassportHref={`/goats/${encodeURIComponent(selectedGoat.goat_id)}`}
	          pageContract={pageContract}
	        />
      ) : null}
    </div>
  );
}

async function HerdPassportDrawer({
  goat,
  closeHref,
  fullPassportHref,
  pageContract,
}: {
  goat: GoatRow;
  closeHref: string;
  fullPassportHref: string;
  pageContract: AdminUiPageContract;
}) {
  const cols = tableLabels(pageContract, "herd-register");
  return (
    <>
      <Link href={closeHref} replace className="veil" aria-label={copy(pageContract, "drawer.passport.close_label")} scroll={false} />
      <aside className="drawer on" aria-label={copy(pageContract, "drawer.passport.aria")}>
        <div className="dh">
          <span className="fic" style={{ background: "var(--brand-soft)", color: "var(--brand-d)", fontWeight: 800 }}>
            G
          </span>
          <div>
            <div className="mt">{goat.display_id}</div>
            <h2>{copy(pageContract, "drawer.passport.aria")}</h2>
          </div>
          <span className="sp" style={{ flex: 1 }} />
          <Link href={closeHref} replace className="iconbtn" aria-label={copy(pageContract, "drawer.passport.close_label")} scroll={false}>
            <X className="ic" />
          </Link>
        </div>
        <div className="dc">
          <div className="helpgrid">
            <div className="hk">{cols[1]}</div>
            <div>{locationLabel(goat, "park")}</div>
            <div className="hk">{cols[2]}</div>
            <div>{locationLabel(goat, "shed")}</div>
            <div className="hk">{cols[3]}</div>
            <div>{dash(goat.breed)}</div>
            <div className="hk">{cols[4]}</div>
            <div>{dash(goat.sex)}</div>
            <div className="hk">{cols[5]}</div>
            <div>{weightLabel(goat.weight_kg)}{goat.weight_kg ? " kg" : ""}</div>
            <div className="hk">{cols[6]}</div>
            <div>
              <Tag tone={statusTone(goat.lifecycle_status, "lifecycle")}>{dash(goat.lifecycle_status)}</Tag>
            </div>
            <div className="hk">{cols[7]}</div>
            <div>
              <Tag tone={statusTone(goat.health_status, "health")}>{dash(goat.health_status)}</Tag>
            </div>
            <div className="hk">{cols[8]}</div>
            <div>
              <Tag tone={statusTone(goat.reproductive_status, "breeding")}>{dash(goat.reproductive_status)}</Tag>
            </div>
          </div>
          <div className="muted small" style={{ marginTop: 14, fontWeight: 700 }}>
            {copy(pageContract, "label.identifiers")}
          </div>
          <div className="note" style={{ marginTop: 8 }}>
            {copy(pageContract, "label.goat_id")}: {goat.goat_id} · {copy(pageContract, "label.display_id")}: {goat.display_id} · {copy(pageContract, "label.animal_identifier_1")}: {dash(goat.animal_identifier_1)} · {copy(pageContract, "label.animal_identifier_2")}: {dash(goat.animal_identifier_2)}
          </div>
          <HerdPassportVaccinationBlock goatId={goat.goat_id} />
        </div>
        <div className="df">
          <Link href={fullPassportHref} className="btn p">
            {copy(pageContract, "action.full_change_history")}
          </Link>
          <Link href={closeHref} replace className="btn" scroll={false}>
            {copy(pageContract, "action.close")}
          </Link>
        </div>
      </aside>
    </>
  );
}
