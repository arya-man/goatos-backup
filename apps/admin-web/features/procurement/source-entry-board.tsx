import Link from "next/link";
import { redirect } from "next/navigation";
import { ArrowRight, PackageSearch, Truck, X } from "lucide-react";
import { INTERNAL_LOGIN_PATH } from "@/lib/auth/session-cookie";
import { firstAuthRequiredError } from "@/lib/api/server";
import { getProcurementLoad, listProcurementLoads } from "@/lib/api/procurement-server";
import type { ProcurementLoad, ProcurementLoadDetail, ProcurementLoadStatus } from "@/lib/api/procurement";
import { boundedInt, hrefPreviousCursor, hrefWithCursor, one, type RouteSearchParams } from "@/lib/search-params";
import { fmtDate, shortId } from "@/lib/format";
import { Tag, type Tone } from "@/components/ui-primitives";
import { PROC_LOAD_STATUS_META, PROC_LOAD_STATUS_ORDER, warmupMeta } from "./work-state";
import { NewLoadForm } from "./load-forms";
import { ProcurementPager } from "./pager";
import { VaccinationFilterButton, VisibleTableSearch } from "@/features/phc-vaccination/vaccination-filter-modal";

const LOADS_COLS = [
  "Load",
  "Holding farm · supplier",
  "Purpose",
  "Animals",
  "Warmup",
  "Tagging",
  "Vaccination · at HF",
  "Health / Selection",
  "Status",
];

// The canonical source-entry journey, shown as IA so users learn that a goat journey starts at
// purchase/source and that park arrival is only one gate near the end. Explanatory copy, not data.
const JOURNEY_STAGES = ["Purchase / source", "Holding warmup", "Source health SOP", "Pre-dispatch", "Transit", "Arrival gate", "Accepted intake"];

function daysSince(date: string | null | undefined): number | null {
  if (!date) return null;
  const start = new Date(`${date}T00:00:00Z`);
  if (Number.isNaN(start.getTime())) return null;
  const diff = Date.now() - start.getTime();
  return Math.max(0, Math.floor(diff / 86_400_000));
}

function warmupCell(load: ProcurementLoad, detail: ProcurementLoadDetail | undefined): { label: string; tone: Tone; note: string } {
  const goats = detail?.goats ?? [];
  const purposeValues = Array.from(new Set(goats.map((g) => g.purpose).filter(Boolean)));
  const goatDays = goats
    .map((g) => g.warmup_days)
    .filter((d): d is number => typeof d === "number");
  const days = goatDays.length > 0 ? Math.max(...goatDays) : daysSince(load.purchase_date);

  if (purposeValues.length > 1) {
    return {
      label: days === null ? "mixed windows" : `${days}d · mixed`,
      tone: "info",
      note: "mixed purpose load — review per-goat warmup in load detail",
    };
  }

  const purpose = purposeValues[0] ?? "unspecified";
  const warm = warmupMeta(days, purpose);
  return {
    label: warm.label === "—" ? "—" : `${warm.label} / ${warm.expectation}`,
    tone: warm.tone,
    note: warm.note ?? warm.expectation,
  };
}

function healthSelectionLabel(status: ProcurementLoadStatus): { label: string; tone: "ok" | "warn" | "dng" | "info" | "mut" | "pur" | "teal" } {
  switch (status) {
    case "source_warmup":
      return { label: "warming", tone: "info" };
    case "health_pending":
      return { label: "health pending", tone: "warn" };
    case "pre_dispatch_pending":
    case "dispatch_ready":
      return { label: "selection ok", tone: "ok" };
    case "rejected":
    case "blocked":
      return { label: "blocked / rejected", tone: "dng" };
    case "deferred":
      return { label: "review", tone: "warn" };
    default:
      return { label: "cleared forward", tone: "ok" };
  }
}

function sourcePartyLabel(load: ProcurementLoad): string {
  return load.source_party_name || shortId(load.source_party_id);
}

function sourceLocationLabel(load: ProcurementLoad): string {
  return load.source_location_name || load.source_location_code || "Holding not set";
}

function hrefWithQuery(pathname: string, params: RouteSearchParams, changes: Record<string, string | null | undefined>): string {
  const next = new URLSearchParams();
  for (const [name, value] of Object.entries(params)) {
    if (Object.prototype.hasOwnProperty.call(changes, name)) continue;
    if (Array.isArray(value)) {
      for (const item of value) if (item) next.append(name, item);
    } else if (value) {
      next.set(name, value);
    }
  }
  for (const [name, value] of Object.entries(changes)) {
    next.delete(name);
    if (value && value !== "all") next.set(name, value);
  }
  const qs = next.toString();
  return qs ? `${pathname}?${qs}` : pathname;
}

function purposeLabel(detail: ProcurementLoadDetail | undefined): string {
  const purposes = Array.from(new Set((detail?.goats ?? []).map((g) => g.purpose).filter(Boolean)));
  const [first] = purposes;
  if (!first) return "—";
  if (purposes.length === 1) return first.replace(/_/g, " ");
  return "mixed";
}

function taggingLabel(detail: ProcurementLoadDetail | undefined, expectedCount: number): string {
  const goats = detail?.goats ?? [];
  const tagged = goats.filter((g) => Boolean(g.source_tag || g.source_rfid || g.temporary_id)).length;
  return `${tagged}/${expectedCount}`;
}

function hfVaccinationLabel(detail: ProcurementLoadDetail | undefined): { label: string; tone: "ok" | "warn" | "dng" | "info" | "mut" | "pur" | "teal" } {
  const evidence = detail?.hf_vaccination_evidence ?? [];
  if (evidence.some((row) => row.review_status === "trusted")) return { label: "complete · evidence", tone: "ok" };
  if (evidence.some((row) => row.review_status === "imported")) return { label: "evidence imported", tone: "info" };
  if (evidence.some((row) => row.review_status === "rejected" || row.review_status === "conflicting")) {
    return { label: "evidence flagged", tone: "dng" };
  }
  return { label: "HF evidence due", tone: "warn" };
}

export async function SourceEntryBoardPage({ searchParams }: { searchParams?: RouteSearchParams }) {
  const sp = searchParams ?? {};
  const pathname = "/procurement/source-entry";
  const statusFilter = (PROC_LOAD_STATUS_ORDER.find((s) => s === one(sp, "status")) ?? "all") as ProcurementLoadStatus | "all";
  const cursor = one(sp, "cursor");
  const page = boundedInt(one(sp, "page"), 1, 1, 1_000_000);
  const PAGE_SIZE = 200;
  const actionStatus = one(sp, "action_status");
  const actionMessage = one(sp, "action_message");
  const selectedLoadId = one(sp, "source_load");

  const result = await listProcurementLoads({
    status: statusFilter === "all" ? undefined : statusFilter,
    limit: PAGE_SIZE,
    cursor,
  });
  const authError = firstAuthRequiredError(result);
  if (authError) redirect(INTERNAL_LOGIN_PATH);

  // Sort the returned page by the canonical stage order so the board reads source-side -> intake.
  const loads: ProcurementLoad[] = result.ok
    ? [...result.data.items].sort((a, b) => PROC_LOAD_STATUS_ORDER.indexOf(a.status) - PROC_LOAD_STATUS_ORDER.indexOf(b.status))
    : [];
  const detailResults = result.ok
    ? await Promise.all(loads.map(async (load) => [load.load_id, await getProcurementLoad(load.load_id)] as const))
    : [];
  const detailByLoad = new Map<string, ProcurementLoadDetail>();
  for (const [loadId, detailResult] of detailResults) {
    if (detailResult.ok) detailByLoad.set(loadId, detailResult.data.detail);
  }
  const nextCursor = result.ok ? result.data.next_cursor ?? null : null;
  const nextHref = hrefWithCursor(pathname, sp, nextCursor);
  const prevHref = hrefPreviousCursor(pathname, sp);
  const selectedLoad = selectedLoadId ? loads.find((load) => load.load_id === selectedLoadId) : undefined;
  const selectedDetail = selectedLoad ? detailByLoad.get(selectedLoad.load_id) : undefined;

  // Status filter resets the cursor/page (a new filter starts a fresh first page).
  function statusHref(status: ProcurementLoadStatus | "all"): string {
    return hrefWithQuery(pathname, sp, {
      status: status === "all" ? null : status,
      cursor: null,
      cursor_stack: null,
      page: null,
      source_load: null,
    });
  }

  return (
    <div className="screen on">
      <div className="phead">
        <div>
          <div className="crumb">
            <b>Procurement</b> · Source entry
          </div>
          <h1>Source Entry Board</h1>
          <div className="sub">
            <b>The goat journey starts at purchase/source</b> — not at park arrival. Loads move through supplier holding,
            source health SOP, and a pre-dispatch decision before any truck. Warmup is purpose-specific:
            <b> breeding 45–70 days</b>; <b>fattening / non-breeding 0–14 days</b>.
            Rejected or unresolved goats stay procurement history; only accepted-intake goats flow to PHC / Parks.
          </div>
        </div>
        <div className="sp" style={{ flex: 1 }} />
      </div>

      {/* Journey IA — the full source-entry chain, so park arrival reads as one late gate, not the start. */}
      <div className="fchipsbar" style={{ marginBottom: 14, flexWrap: "wrap" }} aria-label="Source-entry journey">
        <Truck className="ic" style={{ width: 14, color: "var(--brand-d)" }} aria-hidden="true" />
        {JOURNEY_STAGES.map((stage, i) => (
          <span key={stage} style={{ display: "inline-flex", alignItems: "center", gap: 8 }}>
            <span className="muted small">{stage}</span>
            {i < JOURNEY_STAGES.length - 1 ? <ArrowRight className="ic" style={{ width: 12, opacity: 0.5 }} aria-hidden="true" /> : null}
          </span>
        ))}
      </div>

      {actionStatus ? (
        actionStatus === "success" ? (
          <div className="note" style={{ marginBottom: 14 }}>
            <Tag tone="ok">done</Tag> {actionMessage ?? "Action completed."}
          </div>
        ) : (
          <div className="alert" style={{ marginBottom: 14 }}>
            <b>Action failed</b>&nbsp;{actionMessage ?? actionStatus}
          </div>
        )
      ) : null}

      <NewLoadForm returnTo={hrefWithQuery(pathname, sp, { source_load: null })} />

      {!result.ok ? (
        <div className="alert" style={{ marginBottom: 14 }}>
          <b>{result.error.code ?? result.error.kind}</b>&nbsp;{result.error.message}
        </div>
      ) : null}

      {/* Status filter (server-side ?status). Park/date scope stays in the top bar; this is a page control. */}
      <div className="chipset" style={{ marginBottom: 14 }}>
        <Link href={statusHref("all")} replace scroll={false} className={`chip${statusFilter === "all" ? " on" : ""}`}>
          All states
        </Link>
        {PROC_LOAD_STATUS_ORDER.map((s) => (
          <Link key={s} href={statusHref(s)} replace scroll={false} className={`chip${statusFilter === s ? " on" : ""}`}>
            {PROC_LOAD_STATUS_META[s].label}
          </Link>
        ))}
      </div>

      <section className="card">
        <div className="hd">
          <PackageSearch className="ic" style={{ color: "var(--info)" }} aria-hidden="true" />
          <h3>Supplier warmup — Holding Farm</h3>
          <Tag tone={loads.length ? "info" : "mut"}>journey starts at purchase</Tag>
          <div className="sp" style={{ flex: 1 }} />
          <span className="muted small">purchase → tag + vaccinate → health select → pre-dispatch</span>
        </div>
        <div className="tbar">
          <VisibleTableSearch label="Search source-entry loads" />
          <VaccinationFilterButton
            title="Filter — Source Entry loads"
            searchReason="Search load, supplier, purpose, status..."
            filterReason="Use visible-row search, quick facets, and live status chips on this board."
            rowsLabel={`${loads.length} rows · source loads and HF evidence`}
            facets={["Load", "Supplier", "Purpose", "HF evidence", "Status"]}
          />
          <span className="muted small">{loads.length} rows</span>
          <span className="muted small">click a row → source-load actions</span>
        </div>
        <div style={{ overflowX: "auto" }} tabIndex={0} role="group" aria-label="Procurement loads">
          <table>
            <thead>
              <tr>
                {LOADS_COLS.map((c) => (
                  <th key={c}>{c}</th>
                ))}
              </tr>
            </thead>
            <tbody>
              {loads.length === 0 ? (
                <tr>
                  <td colSpan={LOADS_COLS.length}>
                    <div className="muted small" style={{ padding: "18px 4px", textAlign: "center", lineHeight: 1.6 }}>
                      {result.ok
                        ? statusFilter === "all"
                          ? "No source-entry loads for this scope. Loads appear here once a purchase/source load is created in the procurement backend."
                          : `No loads in “${PROC_LOAD_STATUS_META[statusFilter as ProcurementLoadStatus].label}” for this scope.`
                        : "Loads are unavailable until the procurement service responds."}
                    </div>
                  </td>
                </tr>
              ) : (
                loads.map((load) => {
                  const drawerHref = hrefWithQuery(pathname, sp, { source_load: load.load_id });
                  const healthSelection = healthSelectionLabel(load.status);
                  const detail = detailByLoad.get(load.load_id);
                  const hfVaccination = hfVaccinationLabel(detail);
                  const purpose = purposeLabel(detail);
                  const warmup = warmupCell(load, detail);
                  return (
                    <tr key={load.load_id}>
                      <td>
                        <Link href={drawerHref} className="celllink" scroll={false}>
                          <span className="gid">{shortId(load.load_id)}</span>
                        </Link>
                      </td>
                      <td>
                        <Link href={drawerHref} className="celllink" scroll={false}>
                          <b>{sourceLocationLabel(load)}</b>
                          <div className="muted small">supplier {sourcePartyLabel(load)}</div>
                        </Link>
                      </td>
                      <td>
                        <Link href={drawerHref} className="celllink" scroll={false}>
                          <Tag tone={purpose === "—" || purpose === "fattening" ? "mut" : "ok"}>{purpose}</Tag>
                        </Link>
                      </td>
                      <td>
                        <Link href={drawerHref} className="celllink" scroll={false}>
                          {load.expected_count}
                        </Link>
                      </td>
                      <td>
                        <Link href={drawerHref} className="celllink" scroll={false}>
                          <Tag tone={warmup.tone}>{warmup.label}</Tag>
                          <div className="muted small" title={warmup.note}>
                            {load.purchase_date ? `from ${fmtDate(load.purchase_date)}` : "purchase date missing"}
                          </div>
                        </Link>
                      </td>
                      <td>
                        <Link href={drawerHref} className="celllink" scroll={false}>
                          <Tag tone="mut">{taggingLabel(detail, load.expected_count)}</Tag>
                        </Link>
                      </td>
                      <td>
                        <Link href={drawerHref} className="celllink" scroll={false}>
                          <Tag tone={hfVaccination.tone}>{hfVaccination.label}</Tag>
                        </Link>
                      </td>
                      <td>
                        <Link href={drawerHref} className="celllink" scroll={false}>
                          <Tag tone={healthSelection.tone}>{healthSelection.label}</Tag>
                        </Link>
                      </td>
                      <td>
                        <Link href={drawerHref} className="celllink" scroll={false}>
                          <Tag tone={PROC_LOAD_STATUS_META[load.status].tone}>{PROC_LOAD_STATUS_META[load.status].label}</Tag>
                          <ArrowRight className="ic" style={{ width: 13, flexShrink: 0, marginLeft: 6 }} aria-hidden="true" />
                        </Link>
                      </td>
                    </tr>
                  );
                })
              )}
            </tbody>
          </table>
        </div>
        {loads.length > 0 || page > 1 ? (
          <ProcurementPager prevHref={prevHref} nextHref={nextHref} page={page} count={loads.length} noun="load" />
        ) : null}
      </section>
      {selectedLoad ? (
        <SourceLoadDrawer
          load={selectedLoad}
          detail={selectedDetail}
          closeHref={hrefWithQuery(pathname, sp, { source_load: null })}
          detailHref={hrefWithQuery(`/procurement/source-entry/loads/${encodeURIComponent(selectedLoad.load_id)}`, sp, {
            source_load: null,
            cursor: null,
            cursor_stack: null,
            page: null,
          })}
        />
      ) : null}
    </div>
  );
}

function SourceLoadDrawer({
  load,
  detail,
  closeHref,
  detailHref,
}: {
  load: ProcurementLoad;
  detail: ProcurementLoadDetail | undefined;
  closeHref: string;
  detailHref: string;
}) {
  const healthSelection = healthSelectionLabel(load.status);
  const hfVaccination = hfVaccinationLabel(detail);
  const warmup = warmupCell(load, detail);
  const purpose = purposeLabel(detail);
  const goatsInLoad = detail?.goats?.length ?? 0;
  return (
    <>
      <Link href={closeHref} replace className="veil" aria-label="Close source load drawer" scroll={false} />
      <aside className="drawer on" aria-label="Source load actions">
        <div className="dh">
          <span className="fic" style={{ background: "var(--brand-soft)", color: "var(--info)" }}>
            <PackageSearch className="ic" aria-hidden="true" />
          </span>
          <div>
            <div className="mt">SOURCE LOAD</div>
            <h2>Holding-farm load — {sourceLocationLabel(load)}</h2>
            <div className="muted small" style={{ marginTop: 3 }}>
              {sourcePartyLabel(load)} · {purpose}
            </div>
          </div>
          <span className="sp" style={{ flex: 1 }} />
          <Link href={closeHref} replace className="iconbtn" aria-label="Close source load drawer" scroll={false}>
            <X className="ic" />
          </Link>
        </div>
        <div className="dc">
          <div className="helpgrid">
            <div className="hk">Load</div>
            <div>{shortId(load.load_id)}</div>
            <div className="hk">Supplier</div>
            <div>{sourcePartyLabel(load)}</div>
            <div className="hk">Holding farm</div>
            <div>{sourceLocationLabel(load)}</div>
            <div className="hk">Expected animals</div>
            <div>{load.expected_count}</div>
            <div className="hk">Goats in load</div>
            <div>{goatsInLoad}</div>
            <div className="hk">Warmup</div>
            <div>
              <Tag tone={warmup.tone}>{warmup.label}</Tag>
            </div>
            <div className="hk">Tagging</div>
            <div>
              <Tag tone="mut">{taggingLabel(detail, load.expected_count)}</Tag>
            </div>
            <div className="hk">Vaccination · HF</div>
            <div>
              <Tag tone={hfVaccination.tone}>{hfVaccination.label}</Tag>
            </div>
            <div className="hk">Health / selection</div>
            <div>
              <Tag tone={healthSelection.tone}>{healthSelection.label}</Tag>
            </div>
            <div className="hk">Status</div>
            <div>
              <Tag tone={PROC_LOAD_STATUS_META[load.status].tone}>{PROC_LOAD_STATUS_META[load.status].label}</Tag>
            </div>
          </div>
          <div className="note" style={{ marginTop: 14 }}>
            This load starts before park arrival: purchase/source → holding warmup → HF vaccination evidence → health
            selection → pre-dispatch. Accepted-intake goats then feed the PHC vaccination flow.
          </div>
        </div>
        <div className="df">
          <Link href={detailHref} className="btn p">
            Open load actions
          </Link>
          <Link href={`${detailHref}#hf-evidence`} className="btn">
            Record HF evidence
          </Link>
          <Link href={closeHref} replace className="btn" scroll={false}>
            Close
          </Link>
        </div>
      </aside>
    </>
  );
}
