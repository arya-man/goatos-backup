import Link from "next/link";
import { ArrowRight, Truck, X } from "lucide-react";
import { Tag, type Tone } from "@/components/ui-primitives";
import { firstAuthRequiredError } from "@/lib/api/server";
import { getProcurementLoad, listProcurementLoads } from "@/lib/api/procurement-server";
import type { ProcurementLoad, ProcurementLoadDetail, ProcurementLoadStatus } from "@/lib/api/procurement";
import { fmtDate, shortId } from "@/lib/format";
import { PROC_LOAD_STATUS_META, warmupMeta } from "@/features/procurement/work-state";
import { VaccinationFilterButton, VisibleTableSearch } from "./vaccination-filter-modal";
import { one, type RouteSearchParams } from "@/lib/search-params";
import { scopeHref, type Scope } from "@/lib/scope";

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

function sourcePartyLabel(load: ProcurementLoad): string {
  return load.source_party_name || shortId(load.source_party_id);
}

function sourceLocationLabel(load: ProcurementLoad): string {
  return load.source_location_name || load.source_location_code || "Holding not set";
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

function hfVaccinationLabel(detail: ProcurementLoadDetail | undefined): { label: string; tone: Tone } {
  const evidence = detail?.hf_vaccination_evidence ?? [];
  if (evidence.some((row) => row.review_status === "trusted")) return { label: "complete · evidence", tone: "ok" };
  if (evidence.some((row) => row.review_status === "imported")) return { label: "evidence imported", tone: "info" };
  if (evidence.some((row) => row.review_status === "rejected" || row.review_status === "conflicting")) {
    return { label: "evidence flagged", tone: "dng" };
  }
  return { label: "HF evidence due", tone: "warn" };
}

function healthSelectionLabel(status: ProcurementLoadStatus): { label: string; tone: Tone } {
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

function warmupCell(load: ProcurementLoad, detail: ProcurementLoadDetail | undefined): { label: string; tone: Tone; note: string } {
  const goats = detail?.goats ?? [];
  const purposes = Array.from(new Set(goats.map((g) => g.purpose).filter(Boolean)));
  const goatDays = goats.map((g) => g.warmup_days).filter((d): d is number => typeof d === "number");
  const days = goatDays.length > 0 ? Math.max(...goatDays) : null;
  if (purposes.length > 1) {
    return { label: days === null ? "mixed windows" : `${days}d · mixed`, tone: "info", note: "mixed purpose load — review per-goat warmup in Source Entry" };
  }
  const warm = warmupMeta(days, purposes[0] ?? "unspecified");
  return {
    label: warm.label === "—" ? "—" : `${warm.label} / ${warm.expectation}`,
    tone: warm.tone,
    note: warm.note ?? warm.expectation,
  };
}

export async function SupplierWarmupContext({ scope, searchParams }: { scope: Scope; searchParams?: RouteSearchParams }) {
  const loadsResult = await listProcurementLoads({ limit: 4 });
  const authError = firstAuthRequiredError(loadsResult);

  if (authError) {
    return (
      <section className="card" style={{ marginBottom: 16 }}>
        <div className="hd">
          <Truck className="ic" style={{ color: "var(--info)" }} aria-hidden="true" />
          <h3>Supplier warmup — Holding Farm</h3>
          <Tag tone="mut">source-entry auth required</Tag>
        </div>
        <div className="bd">
          <span className="muted small">Sign in again to view Holding-Farm vaccination evidence.</span>
        </div>
      </section>
    );
  }

  if (!loadsResult.ok) {
    return (
      <section className="card" style={{ marginBottom: 16, borderColor: "color-mix(in srgb,var(--info) 26%,var(--line))" }}>
        <div className="hd">
          <Truck className="ic" style={{ color: "var(--info)" }} aria-hidden="true" />
          <h3>Supplier warmup — Holding Farm</h3>
          <Tag tone="warn">source-entry unavailable</Tag>
        </div>
        <div className="bd">
          <span className="muted small">{loadsResult.error.message}</span>
        </div>
      </section>
    );
  }

  const loads = loadsResult.data.items;
  const detailResults = await Promise.all(loads.map(async (load) => [load.load_id, await getProcurementLoad(load.load_id)] as const));
  const detailByLoad = new Map<string, ProcurementLoadDetail>();
  for (const [loadId, detailResult] of detailResults) {
    if (detailResult.ok) detailByLoad.set(loadId, detailResult.data.detail);
  }
  const selectedLoad = loads.find((load) => load.load_id === one(searchParams ?? {}, "warmup_load"));

  return (
    <>
    <section className="card" style={{ marginBottom: 16, borderColor: "color-mix(in srgb,var(--info) 26%,var(--line))" }}>
      <div className="hd">
        <Truck className="ic" style={{ color: "var(--info)" }} aria-hidden="true" />
        <h3>Supplier warmup — Holding Farm <span className="muted small" style={{ fontWeight: 600 }}>(pre-arrival)</span></h3>
        <div className="sp" style={{ flex: 1 }} />
        <Tag tone="info">journey starts at purchase</Tag>
      </div>
      <div className="note" style={{ margin: "12px 14px 6px" }}>
        Purchased goats start at the supplier / holding farm. HF doses import as completion evidence so accepted-intake
        goats do not double-dose on arrival. Source Entry owns the write actions; PHC reads the evidence here.
      </div>
      <div className="tbar">
        <VisibleTableSearch label="Search supplier warmup loads" />
        <VaccinationFilterButton
          title="Filter — Supplier warmup"
          searchReason="Search holding farm, supplier, purpose, status..."
          filterReason="Use visible-row search and quick facets here; open Source Entry for the full load workflow."
          rowsLabel={`${loads.length} rows · source loads and HF evidence`}
          actionHref="/procurement/source-entry"
          actionLabel="Open Source Entry"
          facets={["Holding farm", "Supplier", "Purpose", "HF evidence", "Health / selection"]}
        />
        <span className="muted small">{loads.length} rows</span>
        <span className="muted small">click a load → actions</span>
      </div>
      <div style={{ overflowX: "auto" }} tabIndex={0} role="group" aria-label="Supplier warmup loads">
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
                  <div className="muted small" style={{ padding: "18px 4px", textAlign: "center" }}>
                    No source-entry loads yet. Holding-Farm evidence appears after a procurement load is created.
                  </div>
                </td>
              </tr>
            ) : (
              loads.map((load) => {
                const drawerHref = scopeHref("/vaccination", scope, {}, { warmup_load: load.load_id });
                const detail = detailByLoad.get(load.load_id);
                const purpose = purposeLabel(detail);
                const warmup = warmupCell(load, detail);
                const hfVaccination = hfVaccinationLabel(detail);
                const healthSelection = healthSelectionLabel(load.status);
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
                        <Tag tone={purpose === "—" ? "mut" : "ok"}>{purpose}</Tag>
                      </Link>
                    </td>
                    <td>
                      <Link href={drawerHref} className="celllink" scroll={false}>
                        {load.expected_count}
                      </Link>
                    </td>
                    <td title={warmup.note}>
                      <Link href={drawerHref} className="celllink" scroll={false}>
                        <Tag tone={warmup.tone}>{warmup.label}</Tag>
                        <div className="muted small">{load.purchase_date ? `from ${fmtDate(load.purchase_date)}` : "purchase date missing"}</div>
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
                        <span style={{ display: "inline-flex", alignItems: "center", gap: 6 }}>
                          <Tag tone={PROC_LOAD_STATUS_META[load.status].tone}>{PROC_LOAD_STATUS_META[load.status].label}</Tag>
                          <ArrowRight className="ic" style={{ width: 13, flexShrink: 0 }} aria-hidden="true" />
                        </span>
                      </Link>
                    </td>
                  </tr>
                );
              })
            )}
          </tbody>
        </table>
      </div>
      <div className="note" style={{ margin: "8px 14px 12px" }}>
        Lifecycle: <b>purchase → supplier warmup (tag + vaccinate · rejectable) → load → arrival → accepted intake → PHC obligations</b>.
        Rejected-before-truck goats never enter park count or active vaccination work.
      </div>
	    </section>
      {selectedLoad ? (
        <WarmupLoadDrawer
          load={selectedLoad}
          detail={detailByLoad.get(selectedLoad.load_id)}
          closeHref={scopeHref("/vaccination", scope)}
        />
      ) : null}
    </>
  );
}

function WarmupLoadDrawer({
  load,
  detail,
  closeHref,
}: {
  load: ProcurementLoad;
  detail?: ProcurementLoadDetail;
  closeHref: string;
}) {
  const href = `/procurement/source-entry/loads/${encodeURIComponent(load.load_id)}`;
  const purpose = purposeLabel(detail);
  const warmup = warmupCell(load, detail);
  const hfVaccination = hfVaccinationLabel(detail);
  return (
    <>
      <Link href={closeHref} replace className="veil" aria-label="Close Holding Farm load drawer" scroll={false} />
      <aside className="drawer on" aria-label="Holding Farm load">
        <div className="dh">
          <span className="fic" style={{ background: "var(--brand-soft)", color: "var(--brand-d)" }}>
            <Truck className="ic" aria-hidden="true" />
          </span>
          <div>
            <div className="mt">WARMUP</div>
            <h2>
              Holding-farm load — {shortId(load.load_id)} · {sourcePartyLabel(load)} · {purpose}
            </h2>
          </div>
          <span className="sp" style={{ flex: 1 }} />
          <Link href={closeHref} replace className="iconbtn" aria-label="Close Holding Farm load drawer" scroll={false}>
            <X className="ic" />
          </Link>
        </div>
        <div className="dc">
          <div className="note">
            Source Entry owns Holding-Farm write actions. PHC reads HF dose evidence here so arrival vaccination never double-doses.
          </div>
          <div className="metagrid" style={{ marginTop: 14 }}>
            <div>
              <div className="k">Animals in load</div>
              <div className="v">{load.expected_count}</div>
            </div>
            <div>
              <div className="k">Holding farm</div>
              <div className="v">{sourceLocationLabel(load)}</div>
            </div>
            <div>
              <div className="k">Purpose</div>
              <div className="v">{purpose}</div>
            </div>
            <div>
              <div className="k">Warmup</div>
              <div className="v">{warmup.label}</div>
            </div>
            <div>
              <div className="k">HF vaccination</div>
              <div className="v">
                <Tag tone={hfVaccination.tone}>{hfVaccination.label}</Tag>
              </div>
            </div>
          </div>
          <div className="muted small" style={{ marginTop: 14, fontWeight: 700 }}>
            Action
          </div>
          <div className="chipset" style={{ marginTop: 8 }}>
            <Link href={`${href}#hf-evidence`} className="chip on">
              Record HF dose
            </Link>
            <Link href={`${href}#hf-evidence`} className="chip">
              Import vaccination evidence
            </Link>
            <Link href={`${href}#review`} className="chip">
              Reject before load
            </Link>
            <Link href={`${href}#dispatch`} className="chip">
              Clear to ship
            </Link>
          </div>
        </div>
        <div className="df">
          <Link href={`${href}#hf-evidence`} className="btn p">
            Open Source Entry
          </Link>
          <Link href={closeHref} replace className="btn" scroll={false}>
            Cancel
          </Link>
        </div>
      </aside>
    </>
  );
}
