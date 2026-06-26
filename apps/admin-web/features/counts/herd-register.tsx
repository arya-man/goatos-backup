import { randomUUID } from "node:crypto";
import Link from "next/link";
import { redirect } from "next/navigation";
import { ChevronLeft, ChevronRight, X } from "lucide-react";

import { Tag } from "@/components/ui-primitives";
import { dash } from "@/lib/format";
import {
  firstAuthRequiredError,
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
import { HerdActions } from "./herd-actions-ui";
import { HerdFiltersModalClient } from "./herd-filters-modal-client";

// Counts -> Herd Register. The vaccination cascade's real business entry point: register/import a goat,
// emit goat.created, generate vaccination obligations. This screen is the OPERATIONAL Counts module surface.
//
// Generated-client status: goat READ and WRITE operation IDs are present and wired. The herd table +
// filters read /goats/search; Register goat and Import sheet open real drawers that post createAdminGoat /
// bulk preview+commit (see herd-actions.ts / herd-actions-ui.tsx). No hand-rolled DTOs, no fake rows.
// New report has no API and stays disabled.

const DEFAULT_PAGE_SIZE = 10;
const SUMMARY_LIMIT = 100;
const PAGE_SIZE_OPTIONS = [10, 25, 50] as const;

type GoatRow = GoatSearchResponse["items"][number];

const REPORT_PENDING = "No herd report API exists in this slice. New report stays disabled.";

const COLS = ["Goat ID", "Park", "Shed", "Breed", "Sex", "WT", "Lifecycle", "Health", "Breeding"];

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

function identityTone(state: GoatRow["identity_state"]): "ok" | "warn" | "dng" | "mut" {
  switch (state) {
    case "clean":
      return "ok";
    case "needs_review":
    case "disputed":
      return "warn";
    case "merged":
    case "inactive":
      return "dng";
    default:
      return "mut";
  }
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
  const display = String(g.display_id ?? "");
  return !display || display.startsWith("TMP-") || g.identity_state !== "clean";
}

function buildHerdSummary(rows: GoatRow[], capped: boolean) {
  const activeRows = rows.filter(isActiveGoat);
  const kidRows = activeRows.filter(isKidGoat);
  const adultRows = activeRows.filter((g) => !isKidGoat(g));
  const untaggedKids = kidRows.filter(isUntagged).length;
  const suffix = capped ? "+" : "";
  const sub = capped ? `first ${rows.length} live rows` : `${rows.length} live rows`;
  return [
    { label: "Active", value: `${activeRows.length}${suffix}`, sub },
    { label: "Adults", value: `${adultRows.length}${suffix}`, sub: "live scoped register" },
    { label: "Kids", value: `${kidRows.length}${suffix}`, sub: "stage/shed inferred" },
    { label: "Untagged kids", value: `${untaggedKids}${suffix}`, sub: "identity review rows" },
  ];
}

export async function HerdRegisterPage({ searchParams }: { searchParams?: RouteSearchParams }) {
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
  const pageSize = PAGE_SIZE_OPTIONS.includes(Number(one(sp, "limit")) as (typeof PAGE_SIZE_OPTIONS)[number])
    ? (Number(one(sp, "limit")) as (typeof PAGE_SIZE_OPTIONS)[number])
    : DEFAULT_PAGE_SIZE;
  const cursor = one(sp, "cursor");
  const page = boundedInt(one(sp, "page"), 1, 1, 1_000_000);
  const hasFilter = Boolean(q || breed || sex);
  const actionStatus = one(sp, "action_status");
  const actionMessage = one(sp, "action_message");
  const returnTo = hrefWithoutAction(pathname, sp);
  const selectedGoatId = one(sp, "goat_passport");

  // Real goats + real location options for the write drawers, in parallel.
  const [result, summaryResult, locations] = await Promise.all([
    searchGoats({ limit: pageSize, cursor, q, breed, sex, park_id: parkId }),
    searchGoats({ limit: SUMMARY_LIMIT, q, breed, sex, park_id: parkId }),
    getHerdRegisterLocations(),
  ]);
  const authError = firstAuthRequiredError(result, summaryResult);
  if (authError) redirect(INTERNAL_LOGIN_PATH);

  // A fresh idempotency key per render: a double-submit of the open Register drawer replays the same key
  // (backend returns the original goat); a reload mints a new key for a new logical create.
  const registerIdempotencyKey = randomUUID();

  const goats: GoatRow[] = result.ok ? result.data.items : [];
  const summaryRows: GoatRow[] = summaryResult.ok ? summaryResult.data.items : goats;
  const summaryCards = buildHerdSummary(summaryRows, Boolean(summaryResult.ok && summaryResult.data.next_cursor));
  const nextCursor = result.ok ? result.data.next_cursor ?? null : null;
  const nextHref = hrefWithCursor(pathname, sp, nextCursor);
  const prevHref = hrefPreviousCursor(pathname, sp);
  const scopedPark = parkId ? locations.parks.find((p) => p.id === parkId) : null;
  const herdContext = scopedPark ? `${scopedPark.code ?? scopedPark.name} · all sheds` : "all parks";
  const selectedGoat = selectedGoatId ? goats.find((g) => g.goat_id === selectedGoatId) : undefined;
  const closePassportHref = hrefWithDrawerParam(pathname, sp, "goat_passport", null);

  return (
    <div className="screen on">
      <div className="phead">
        <div>
          <div className="crumb">
            Counts / <b>Herd</b>
          </div>
          <h1>Herd &amp; Lifecycle</h1>
        </div>
        <div className="sp" style={{ flex: 1 }} />
        {/* Import sheet + Register goat open real drawers wired to the generated admin goat clients. */}
        <HerdActions
          parks={locations.parks}
          sheds={locations.sheds}
          farms={locations.farms}
          locationsAvailable={locations.available}
          idempotencyKey={registerIdempotencyKey}
          returnTo={returnTo}
        />
        <button
          type="button"
          className="btn"
          disabled
          aria-disabled="true"
          title={REPORT_PENDING}
          style={{ opacity: 0.5, cursor: "not-allowed" }}
        >
          New report
        </button>
      </div>

      {actionStatus ? (
        actionStatus === "success" ? (
          <div className="note" style={{ marginBottom: 12 }}>
            <Tag tone="ok">done</Tag> {actionMessage ?? "Goat registered."}
          </div>
        ) : (
          <div className="alert" style={{ marginBottom: 12 }}>
            <b>Registration failed</b>&nbsp;{actionMessage ?? actionStatus}
          </div>
        )
      ) : null}

      <div className="grid g4" style={{ marginBottom: 8 }}>
        {summaryCards.map((card) => (
          <div key={card.label} className="kpi" title="Live scoped summary from /goats/search.">
            <div className="lab">{card.label}</div>
            <div className="val">{card.value}</div>
            <div className="dl">
              <span className="muted">{card.sub}</span>
            </div>
          </div>
        ))}
      </div>
      <div className="note" style={{ marginBottom: 16 }}>
        Herd summary and table read live goats from <b>/goats/search</b> under the top-bar scope.
      </div>

      {!result.ok ? (
        <div className="alert" style={{ marginBottom: 16 }}>
          <b>{result.error.code ?? result.error.kind}</b>&nbsp;{result.error.message}
        </div>
      ) : null}

      <section className="card">
        <div className="hd">
          <h3>Herd</h3>
          <span className="small muted">{herdContext}</span>
          <div className="sp" style={{ flex: 1 }} />
          <span className="muted small">tap a row → Goat Passport</span>
        </div>
        <HerdFiltersModalClient rowCount={goats.length} pageSize={pageSize} hasFilters={hasFilter} />
        <div className="bd" style={{ padding: 0, overflowX: "auto" }} tabIndex={0} role="group" aria-label="Herd">
          <table>
            <thead>
              <tr>
                {COLS.map((c) => (
                  <th key={c}>{c}</th>
                ))}
              </tr>
            </thead>
            <tbody>
              {goats.length === 0 ? (
                <tr>
                  <td colSpan={COLS.length}>
                    <div className="muted small" style={{ padding: "18px 4px", textAlign: "center", lineHeight: 1.6 }}>
                      {result.ok
                        ? hasFilter
                          ? "No goats match these filters for this scope."
                          : "No goats for this scope yet. Use Register goat or Import sheet to add the first goats — each emits goat.created and generates vaccination obligations."
                        : "Herd is unavailable until the goats read API responds."}
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
                          {g.identity_state !== "clean" ? (
                            <>
                              {" "}
                              <Tag tone={identityTone(g.identity_state)} title="Identity state from the goats read model">
                                {g.identity_state.replace("_", " ")}
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
              Page {page} · {goats.length} row{goats.length === 1 ? "" : "s"}
              {nextCursor ? " · server-paginated at scale" : " · end of results"}
            </span>
            {prevHref ? (
              <Link href={prevHref} scroll={false} className="btn sm">
                <ChevronLeft className="ic" style={{ width: 13 }} aria-hidden="true" /> Previous
              </Link>
            ) : (
              <span className="btn sm" aria-disabled="true" style={{ opacity: 0.45, cursor: "not-allowed" }}>
                <ChevronLeft className="ic" style={{ width: 13 }} aria-hidden="true" /> Previous
              </span>
            )}
            {nextHref ? (
              <Link href={nextHref} scroll={false} className="btn sm">
                Next <ChevronRight className="ic" style={{ width: 13 }} aria-hidden="true" />
              </Link>
            ) : (
              <span className="btn sm" aria-disabled="true" style={{ opacity: 0.45, cursor: "not-allowed" }}>
                Next <ChevronRight className="ic" style={{ width: 13 }} aria-hidden="true" />
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
        />
      ) : null}
    </div>
  );
}

function HerdPassportDrawer({
  goat,
  closeHref,
  fullPassportHref,
}: {
  goat: GoatRow;
  closeHref: string;
  fullPassportHref: string;
}) {
  return (
    <>
      <Link href={closeHref} replace className="veil" aria-label="Close goat passport drawer" scroll={false} />
      <aside className="drawer on" aria-label="Goat Passport">
        <div className="dh">
          <span className="fic" style={{ background: "var(--brand-soft)", color: "var(--brand-d)", fontWeight: 800 }}>
            G
          </span>
          <div>
            <div className="mt">{goat.display_id}</div>
            <h2>Goat Passport</h2>
          </div>
          <span className="sp" style={{ flex: 1 }} />
          <Link href={closeHref} replace className="iconbtn" aria-label="Close goat passport drawer" scroll={false}>
            <X className="ic" />
          </Link>
        </div>
        <div className="dc">
          <div className="helpgrid">
            <div className="hk">Park</div>
            <div>{locationLabel(goat, "park")}</div>
            <div className="hk">Shed</div>
            <div>{locationLabel(goat, "shed")}</div>
            <div className="hk">Breed</div>
            <div>{dash(goat.breed)}</div>
            <div className="hk">Sex</div>
            <div>{dash(goat.sex)}</div>
            <div className="hk">WT</div>
            <div>{weightLabel(goat.weight_kg)}{goat.weight_kg ? " kg" : ""}</div>
            <div className="hk">Lifecycle</div>
            <div>
              <Tag tone={statusTone(goat.lifecycle_status, "lifecycle")}>{dash(goat.lifecycle_status)}</Tag>
            </div>
            <div className="hk">Health</div>
            <div>
              <Tag tone={statusTone(goat.health_status, "health")}>{dash(goat.health_status)}</Tag>
            </div>
            <div className="hk">Breeding</div>
            <div>
              <Tag tone={statusTone(goat.reproductive_status, "breeding")}>{dash(goat.reproductive_status)}</Tag>
            </div>
          </div>
          <div className="muted small" style={{ marginTop: 14, fontWeight: 700 }}>
            Identifiers
          </div>
          <div className="note" style={{ marginTop: 8 }}>
            goat_id: {goat.goat_id} · display_id: {goat.display_id} · identity: {goat.identity_state.replace("_", " ")}
          </div>
        </div>
        <div className="df">
          <Link href={fullPassportHref} className="btn p">
            Full change history
          </Link>
          <Link href={closeHref} replace className="btn" scroll={false}>
            Close
          </Link>
        </div>
      </aside>
    </>
  );
}
