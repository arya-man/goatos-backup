import Link from "@/components/no-prefetch-link";
import { LocalOverlayLink } from "@/components/local-overlay-link";
import { LocalOverlayDrawer, type LocalOverlayDrawerItem } from "@/components/local-overlay-drawer";
import { ArrowRight, Truck } from "lucide-react";
import { Tag, type Tone } from "@/components/ui-primitives";
import { firstAuthRequiredError } from "@/lib/api/server";
import { getProcurementLoad, listProcurementLoads } from "@/lib/api/procurement-server";
import type { ProcurementLoad, ProcurementLoadDetail, ProcurementLoadStatus } from "@/lib/api/procurement";
import { fmtDate, shortId } from "@/lib/format";
import { warmupMeta } from "@/features/procurement";
import { copy, optionGroup, optionLabel, optionTone, tableLabels, tablePageSizes, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import { VaccinationFilterButton, VisibleTableSearch } from "./vaccination-filter-modal";
import { one, type RouteSearchParams } from "@/lib/search-params";
import { scopeHref, type Scope } from "@/lib/scope";
import { paginateRows, VaccinationTablePager, type VaccinationPageSize } from "./table-pager";

function sourcePartyLabel(load: ProcurementLoad): string {
  return load.source_party_name || shortId(load.source_party_id);
}

function sourceLocationLabel(pageContract: AdminUiPageContract, load: ProcurementLoad): string {
  return load.source_location_name || load.source_location_code || copy(pageContract, "label.holding_not_set");
}

function purposeLabel(pageContract: AdminUiPageContract, detail: ProcurementLoadDetail | undefined): string {
  const purposes = Array.from(new Set((detail?.goats ?? []).map((g) => g.purpose).filter(Boolean)));
  const [first] = purposes;
  if (!first) return copy(pageContract, "label.placeholder");
  if (purposes.length === 1) return first.replace(/_/g, " ");
  return copy(pageContract, "label.mixed");
}

function taggingLabel(detail: ProcurementLoadDetail | undefined, expectedCount: number): string {
  const goats = detail?.goats ?? [];
  const tagged = goats.filter((g) => Boolean(g.animal_identifier_1 && g.animal_identifier_2)).length;
  return `${tagged}/${expectedCount}`;
}

function hfVaccinationLabel(pageContract: AdminUiPageContract, detail: ProcurementLoadDetail | undefined): { label: string; tone: Tone } {
  const evidence = detail?.hf_vaccination_evidence ?? [];
  if (evidence.some((row) => row.review_status === "trusted")) return { label: optionLabel(pageContract, "warmup_evidence_states", "trusted"), tone: optionTone(pageContract, "warmup_evidence_states", "trusted") as Tone };
  if (evidence.some((row) => row.review_status === "imported")) return { label: optionLabel(pageContract, "warmup_evidence_states", "imported"), tone: optionTone(pageContract, "warmup_evidence_states", "imported") as Tone };
  if (evidence.some((row) => row.review_status === "rejected" || row.review_status === "conflicting")) {
    return { label: optionLabel(pageContract, "warmup_evidence_states", "flagged"), tone: optionTone(pageContract, "warmup_evidence_states", "flagged") as Tone };
  }
  return { label: optionLabel(pageContract, "warmup_evidence_states", "due"), tone: optionTone(pageContract, "warmup_evidence_states", "due") as Tone };
}

function healthSelectionLabel(pageContract: AdminUiPageContract, status: ProcurementLoadStatus): { label: string; tone: Tone } {
  switch (status) {
    case "source_warmup":
      return { label: optionLabel(pageContract, "health_selection_states", "warming"), tone: optionTone(pageContract, "health_selection_states", "warming") as Tone };
    case "health_pending":
      return { label: optionLabel(pageContract, "health_selection_states", "health_pending"), tone: optionTone(pageContract, "health_selection_states", "health_pending") as Tone };
    case "pre_dispatch_pending":
    case "dispatch_ready":
      return { label: optionLabel(pageContract, "health_selection_states", "selection_ok"), tone: optionTone(pageContract, "health_selection_states", "selection_ok") as Tone };
    case "rejected":
    case "blocked":
      return { label: optionLabel(pageContract, "health_selection_states", "blocked_rejected"), tone: optionTone(pageContract, "health_selection_states", "blocked_rejected") as Tone };
    case "deferred":
      return { label: optionLabel(pageContract, "health_selection_states", "review"), tone: optionTone(pageContract, "health_selection_states", "review") as Tone };
    default:
      return { label: optionLabel(pageContract, "health_selection_states", "cleared_forward"), tone: optionTone(pageContract, "health_selection_states", "cleared_forward") as Tone };
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

export async function SupplierWarmupContext({ scope, searchParams, pageContract }: { scope: Scope; searchParams?: RouteSearchParams; pageContract: AdminUiPageContract }) {
  const sp = searchParams ?? {};
  const loadsResult = await listProcurementLoads({ limit: 50 });
  const authError = firstAuthRequiredError(loadsResult);
  const labels = tableLabels(pageContract, "supplier-warmup");
  const pageSizeOptions = tablePageSizes(pageContract, "supplier-warmup");

  if (authError) {
    return (
      <section className="card" style={{ marginBottom: 16 }}>
        <div className="hd">
          <Truck className="ic" style={{ color: "var(--info)" }} aria-hidden="true" />
          <h3>{copy(pageContract, "section.supplier_warmup.title")}</h3>
          <Tag tone="mut">{copy(pageContract, "section.supplier_warmup.auth_tag")}</Tag>
        </div>
        <div className="bd">
          <span className="muted small">{copy(pageContract, "section.supplier_warmup.auth_body")}</span>
        </div>
      </section>
    );
  }

  if (!loadsResult.ok) {
    return (
      <section className="card" style={{ marginBottom: 16, borderColor: "color-mix(in srgb,var(--info) 26%,var(--line))" }}>
        <div className="hd">
          <Truck className="ic" style={{ color: "var(--info)" }} aria-hidden="true" />
          <h3>{copy(pageContract, "section.supplier_warmup.title")}</h3>
          <Tag tone="warn">{copy(pageContract, "section.supplier_warmup.unavailable_tag")}</Tag>
        </div>
        <div className="bd">
          <span className="muted small">{loadsResult.error.message}</span>
        </div>
      </section>
    );
  }

  const loads = loadsResult.data.items;
  const paged = paginateRows(loads, sp, "warmup", 5, pageSizeOptions);
  const detailResults = await Promise.all(paged.items.map(async (load) => [load.load_id, await getProcurementLoad(load.load_id)] as const));
  const detailByLoad = new Map<string, ProcurementLoadDetail>();
  for (const [loadId, detailResult] of detailResults) {
    if (detailResult.ok) detailByLoad.set(loadId, detailResult.data.detail);
  }
  const selectedLoadId = one(sp, "warmup_load");
  function pagerHref(page: number): string {
    return scopeHref("/vaccination", scope, {}, { warmup_page: String(page), warmup_limit: String(paged.pageSize) });
  }
  function pageSizeHref(pageSize: VaccinationPageSize): string {
    return scopeHref("/vaccination", scope, {}, { warmup_page: "1", warmup_limit: String(pageSize) });
  }

  return (
    <>
    <section className="card" style={{ marginBottom: 16, borderColor: "color-mix(in srgb,var(--info) 26%,var(--line))" }}>
      <div className="hd">
        <Truck className="ic" style={{ color: "var(--info)" }} aria-hidden="true" />
        <h3>{copy(pageContract, "section.supplier_warmup.title")} <span className="muted small" style={{ fontWeight: 600 }}>({copy(pageContract, "label.pre_arrival")})</span></h3>
        <div className="sp" style={{ flex: 1 }} />
        <Tag tone="info">{copy(pageContract, "section.supplier_warmup.badge")}</Tag>
      </div>
      <div className="note" style={{ margin: "12px 14px 6px" }}>
        {copy(pageContract, "section.supplier_warmup.note")}
      </div>
      <div className="tbar">
        <VisibleTableSearch pageContract={pageContract} label={copy(pageContract, "filter.supplier.search")} />
        <VaccinationFilterButton
          pageContract={pageContract}
          title={copy(pageContract, "filter.supplier.title")}
          searchReason={copy(pageContract, "filter.supplier.reason")}
          filterReason={copy(pageContract, "filter.supplier.filter_reason")}
          rowsLabel={`${paged.start}-${paged.end} ${copy(pageContract, "pager.of")} ${loads.length} ${copy(pageContract, "pager.rows").toLowerCase()} · ${copy(pageContract, "filter.supplier.rows_suffix")}`}
          actionHref="/procurement/source-entry"
          actionLabel={copy(pageContract, "action.open_source_entry")}
          facets={optionGroup(pageContract, "supplier_warmup_facets").map((facet) => facet.label)}
        />
        <span className="muted small">
          {paged.start}-{paged.end} {copy(pageContract, "pager.of")} {loads.length} {copy(pageContract, "pager.rows").toLowerCase()}
        </span>
        <span className="muted small">{copy(pageContract, "section.supplier_warmup.row_hint")}</span>
      </div>
      <div style={{ overflowX: "auto" }} tabIndex={0} role="group" aria-label={copy(pageContract, "section.supplier_warmup.aria")}>
        <table>
          <thead>
            <tr>
              {labels.map((c) => (
                <th key={c}>{c}</th>
              ))}
            </tr>
          </thead>
          <tbody>
            {loads.length === 0 ? (
              <tr>
                <td colSpan={labels.length}>
                  <div className="muted small" style={{ padding: "18px 4px", textAlign: "center" }}>
                    {copy(pageContract, "empty.supplier_warmup")}
                  </div>
                </td>
              </tr>
            ) : (
              paged.items.map((load) => {
                const drawerHref = scopeHref("/vaccination", scope, {}, { warmup_load: load.load_id });
                const detail = detailByLoad.get(load.load_id);
                const purpose = purposeLabel(pageContract, detail);
                const warmup = warmupCell(load, detail);
                const hfVaccination = hfVaccinationLabel(pageContract, detail);
                const healthSelection = healthSelectionLabel(pageContract, load.status);
                return (
                  <tr key={load.load_id}>
                    <td>
                      <LocalOverlayLink href={drawerHref} className="celllink" scroll={false}>
                        <span className="gid">{shortId(load.load_id)}</span>
                      </LocalOverlayLink>
                    </td>
                    <td>
                      <LocalOverlayLink href={drawerHref} className="celllink" scroll={false}>
                        <b>{sourceLocationLabel(pageContract, load)}</b>
                        <div className="muted small">{copy(pageContract, "label.supplier_prefix")} {sourcePartyLabel(load)}</div>
                      </LocalOverlayLink>
                    </td>
                    <td>
                      <LocalOverlayLink href={drawerHref} className="celllink" scroll={false}>
                        <Tag tone={purpose === copy(pageContract, "label.placeholder") ? "mut" : "ok"}>{purpose}</Tag>
                      </LocalOverlayLink>
                    </td>
                    <td>
                      <LocalOverlayLink href={drawerHref} className="celllink" scroll={false}>
                        {load.expected_count}
                      </LocalOverlayLink>
                    </td>
                    <td title={warmup.note}>
                      <LocalOverlayLink href={drawerHref} className="celllink" scroll={false}>
                        <Tag tone={warmup.tone}>{warmup.label}</Tag>
                        <div className="muted small">{load.purchase_date ? `${copy(pageContract, "label.from_date_prefix")} ${fmtDate(load.purchase_date)}` : copy(pageContract, "label.purchase_date_missing")}</div>
                      </LocalOverlayLink>
                    </td>
                    <td>
                      <LocalOverlayLink href={drawerHref} className="celllink" scroll={false}>
                        <Tag tone="mut">{taggingLabel(detail, load.expected_count)}</Tag>
                      </LocalOverlayLink>
                    </td>
                    <td>
                      <LocalOverlayLink href={drawerHref} className="celllink" scroll={false}>
                        <Tag tone={hfVaccination.tone}>{hfVaccination.label}</Tag>
                      </LocalOverlayLink>
                    </td>
                    <td>
                      <LocalOverlayLink href={drawerHref} className="celllink" scroll={false}>
                        <Tag tone={healthSelection.tone}>{healthSelection.label}</Tag>
                      </LocalOverlayLink>
                    </td>
                    <td>
                      <LocalOverlayLink href={drawerHref} className="celllink" scroll={false}>
                        <span style={{ display: "inline-flex", alignItems: "center", gap: 6 }}>
                          <Tag tone={optionTone(pageContract, "source_load_status", load.status) as Tone}>{optionLabel(pageContract, "source_load_status", load.status)}</Tag>
                          <ArrowRight className="ic" style={{ width: 13, flexShrink: 0 }} aria-hidden="true" />
                        </span>
                      </LocalOverlayLink>
                    </td>
                  </tr>
                );
              })
            )}
          </tbody>
        </table>
      </div>
      <div className="note" style={{ margin: "8px 14px 12px" }}>
        {copy(pageContract, "section.supplier_warmup.lifecycle")}
      </div>
      <VaccinationTablePager
        pageContract={pageContract}
        pageSizeOptions={pageSizeOptions}
        page={paged.page}
        pageSize={paged.pageSize}
        total={paged.total}
        start={paged.start}
        end={paged.end}
        noun={labels[0].toLowerCase()}
        hrefForPage={pagerHref}
        hrefForPageSize={pageSizeHref}
      />
	    </section>
      <LocalOverlayDrawer
        items={loads.map((load) => warmupLoadDrawerItem(load, detailByLoad.get(load.load_id), pageContract))}
        selectionKey="warmup_load"
        initialSelectedId={selectedLoadId}
        closeHref={scopeHref("/vaccination", scope)}
        ariaLabel={copy(pageContract, "drawer.warmup.aria")}
        closeLabel={copy(pageContract, "drawer.warmup.close_label")}
      />
    </>
  );
}

function warmupLoadDrawerItem(load: ProcurementLoad, detail: ProcurementLoadDetail | undefined, pageContract: AdminUiPageContract): LocalOverlayDrawerItem {
  const href = `/procurement/source-entry/loads/${encodeURIComponent(load.load_id)}`;
  const purpose = purposeLabel(pageContract, detail);
  const warmup = warmupCell(load, detail);
  const hfVaccination = hfVaccinationLabel(pageContract, detail);
  return {
    id: load.load_id,
    eyebrow: copy(pageContract, "drawer.warmup.eyebrow"),
    title: `${copy(pageContract, "drawer.warmup.title_prefix")} — ${shortId(load.load_id)} · ${sourcePartyLabel(load)} · ${purpose}`,
    icon: <Truck className="ic" aria-hidden="true" />,
    body: (
      <>
          <div className="note">
            {copy(pageContract, "drawer.warmup.note")}
          </div>
          <div className="metagrid" style={{ marginTop: 14 }}>
            <div>
              <div className="k">{copy(pageContract, "drawer.warmup.animals")}</div>
              <div className="v">{load.expected_count}</div>
            </div>
            <div>
              <div className="k">{copy(pageContract, "drawer.warmup.holding_farm")}</div>
              <div className="v">{sourceLocationLabel(pageContract, load)}</div>
            </div>
            <div>
              <div className="k">{copy(pageContract, "drawer.warmup.purpose")}</div>
              <div className="v">{purpose}</div>
            </div>
            <div>
              <div className="k">{copy(pageContract, "drawer.warmup.warmup")}</div>
              <div className="v">{warmup.label}</div>
            </div>
            <div>
              <div className="k">{copy(pageContract, "drawer.warmup.hf_vaccination")}</div>
              <div className="v">
                <Tag tone={hfVaccination.tone}>{hfVaccination.label}</Tag>
              </div>
            </div>
          </div>
          <div className="muted small" style={{ marginTop: 14, fontWeight: 700 }}>
            {copy(pageContract, "drawer.warmup.actions_label")}
          </div>
          <div className="chipset" style={{ marginTop: 8 }}>
            <Link href={`${href}#hf-evidence`} className="chip on">
              {copy(pageContract, "action.record_hf_dose")}
            </Link>
            <Link href={`${href}#hf-evidence`} className="chip">
              {copy(pageContract, "action.import_vaccination_evidence")}
            </Link>
            <Link href={`${href}#review`} className="chip">
              {copy(pageContract, "action.reject_before_load")}
            </Link>
            <Link href={`${href}#dispatch`} className="chip">
              {copy(pageContract, "action.clear_to_ship")}
            </Link>
          </div>
      </>
    ),
    footer: (
          <Link href={`${href}#hf-evidence`} className="btn p">
            {copy(pageContract, "action.open_source_entry")}
          </Link>
    ),
  };
}
