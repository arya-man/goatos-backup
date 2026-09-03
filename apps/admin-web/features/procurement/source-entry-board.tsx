import Link from "@/components/no-prefetch-link";
import { LocalOverlayLink } from "@/components/local-overlay-link";
import { redirect } from "next/navigation";
import { ArrowRight, PackageSearch, Truck } from "lucide-react";
import { INTERNAL_LOGIN_PATH } from "@/lib/auth/session-cookie";
import { firstAuthRequiredError } from "@/lib/api/server";
import { getProcurementLoad, listProcurementLoads } from "@/lib/api/procurement-server";
import type { ProcurementLoad, ProcurementLoadDetail, ProcurementLoadStatus } from "@/lib/api/procurement";
import { boundedInt, hrefPreviousCursor, hrefWithCursor, one, type RouteSearchParams } from "@/lib/search-params";
import { fmtDate, shortId } from "@/lib/format";
import { Tag, type Tone } from "@/components/ui-primitives";
import { actionFeedbackCopy, copy, optionGroup, optionLabel, optionTitle, optionTone, tableLabels, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import { warmupMeta } from "./work-state";
import { NewLoadForm } from "./load-forms";
import { ProcurementPager } from "./pager";
import { SourceEntryLocalDrawer, type SourceEntryDrawerItem } from "./source-entry-local-drawer";
import { VaccinationFilterButton, VisibleTableSearch } from "@/features/preventive-care-vaccination";

function daysSince(date: string | null | undefined): number | null {
  if (!date) return null;
  const start = new Date(`${date}T00:00:00Z`);
  if (Number.isNaN(start.getTime())) return null;
  const diff = Date.now() - start.getTime();
  return Math.max(0, Math.floor(diff / 86_400_000));
}

function contractTone(pageContract: AdminUiPageContract, groupId: string, key: string): Tone {
  return optionTone(pageContract, groupId, key) as Tone;
}

function warmupExpectationKey(purpose: string): string {
  if (purpose === "fattening" || purpose === "non_breeding" || purpose === "breeding") return purpose;
  return "unspecified";
}

function warmupCell(load: ProcurementLoad, detail: ProcurementLoadDetail | undefined, pageContract: AdminUiPageContract): { label: string; tone: Tone; note: string } {
  const goats = detail?.goats ?? [];
  const purposeValues = Array.from(new Set(goats.map((g) => g.purpose).filter(Boolean)));
  const goatDays = goats
    .map((g) => g.warmup_days)
    .filter((d): d is number => typeof d === "number");
  const days = goatDays.length > 0 ? Math.max(...goatDays) : daysSince(load.purchase_date);

  if (purposeValues.length > 1) {
    return {
      label: days === null ? copy(pageContract, "label.mixed_windows") : `${days}d · ${copy(pageContract, "label.mixed")}`,
      tone: "info",
      note: copy(pageContract, "warmup.mixed_note"),
    };
  }

  const purpose = purposeValues[0] ?? "unspecified";
  const warm = warmupMeta(days, purpose);
  const expectationKey = warmupExpectationKey(purpose);
  return {
    label: warm.label === "—" ? copy(pageContract, "label.placeholder") : `${warm.label} / ${optionLabel(pageContract, "warmup_expectations", expectationKey)}`,
    tone: warm.tone,
    note: optionTitle(pageContract, "warmup_expectations", expectationKey),
  };
}

function healthSelectionLabel(status: ProcurementLoadStatus, pageContract: AdminUiPageContract): { label: string; tone: Tone } {
  let key = "cleared_forward";
  switch (status) {
    case "source_warmup":
      key = "warming";
      break;
    case "health_pending":
      key = "health_pending";
      break;
    case "pre_dispatch_pending":
    case "dispatch_ready":
      key = "selection_ok";
      break;
    case "rejected":
    case "blocked":
      key = "blocked_rejected";
      break;
    case "deferred":
      key = "review";
      break;
  }
  return { label: optionLabel(pageContract, "health_selection_states", key), tone: contractTone(pageContract, "health_selection_states", key) };
}

function sourcePartyLabel(load: ProcurementLoad): string {
  return load.source_party_name || shortId(load.source_party_id);
}

function sourceLocationLabel(load: ProcurementLoad, pageContract: AdminUiPageContract): string {
  return load.source_location_name || load.source_location_code || copy(pageContract, "label.holding_not_set");
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

function purposeLabel(detail: ProcurementLoadDetail | undefined, pageContract: AdminUiPageContract): string {
  const purposes = Array.from(new Set((detail?.goats ?? []).map((g) => g.purpose).filter(Boolean)));
  const [first] = purposes;
  if (!first) return copy(pageContract, "label.placeholder");
  if (purposes.length === 1) return optionLabel(pageContract, "proc_purpose", first);
  return copy(pageContract, "label.mixed");
}

function taggingLabel(detail: ProcurementLoadDetail | undefined, expectedCount: number, pageContract: AdminUiPageContract): string {
  if (!detail) return copy(pageContract, "label.placeholder");
	const goats = detail?.goats ?? [];
	const tagged = goats.filter((g) => Boolean(g.animal_identifier_1 && g.animal_identifier_2)).length;
	return `${tagged}/${expectedCount}`;
}

function hfVaccinationLabel(detail: ProcurementLoadDetail | undefined, pageContract: AdminUiPageContract): { label: string; tone: Tone } {
  if (!detail) {
    return { label: copy(pageContract, "label.placeholder"), tone: "mut" };
  }
  const evidence = detail?.hf_vaccination_evidence ?? [];
  let key = "due";
  if (evidence.some((row) => row.review_status === "trusted")) key = "trusted";
  else if (evidence.some((row) => row.review_status === "imported")) key = "imported";
  if (evidence.some((row) => row.review_status === "rejected" || row.review_status === "conflicting")) {
    key = "flagged";
  }
  return { label: optionLabel(pageContract, "warmup_evidence_states", key), tone: contractTone(pageContract, "warmup_evidence_states", key) };
}

export async function SourceEntryBoardPage({
  searchParams,
  pageContract,
}: {
  searchParams?: RouteSearchParams;
  pageContract: AdminUiPageContract;
}) {
  const sp = searchParams ?? {};
  const pathname = "/procurement/source-entry";
  const sourceLoadStatuses = optionGroup(pageContract, "source_load_status");
  const sourceLoadStatusOrder = sourceLoadStatuses.map((status) => status.key as ProcurementLoadStatus);
  const statusFilter = (sourceLoadStatusOrder.find((s) => s === one(sp, "status")) ?? "all") as ProcurementLoadStatus | "all";
  const cursor = one(sp, "cursor");
  const page = boundedInt(one(sp, "page"), 1, 1, 1_000_000);
  const PAGE_SIZE = 200;
  const actionStatus = one(sp, "action_status");
  const actionKey = one(sp, "action_key");
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
    ? [...result.data.items].sort((a, b) => sourceLoadStatusOrder.indexOf(a.status) - sourceLoadStatusOrder.indexOf(b.status))
    : [];
  // request-plan:ignore owner=procurement-platform issue=C35-016 expires=2026-09-30 reason=list contract lacks card facets; replace with enriched paged list or batch detail API
  const detailByLoad = new Map<string, ProcurementLoadDetail>();
  if (selectedLoadId && loads.some((load) => load.load_id === selectedLoadId)) {
    const selectedDetail = await getProcurementLoad(selectedLoadId);
    if (selectedDetail.ok) detailByLoad.set(selectedLoadId, selectedDetail.data.detail);
  }
  const nextCursor = result.ok ? result.data.next_cursor ?? null : null;
  const nextHref = hrefWithCursor(pathname, sp, nextCursor);
  const prevHref = hrefPreviousCursor(pathname, sp);
  const loadLabels = tableLabels(pageContract, "source-loads");
  const journeyStages = optionGroup(pageContract, "journey_stages");
  const closeDrawerHref = hrefWithQuery(pathname, sp, { source_load: null });
  const drawerItems: SourceEntryDrawerItem[] = loads.map((load) => {
    const detail = detailByLoad.get(load.load_id);
    return {
      id: load.load_id,
      sourceLocation: sourceLocationLabel(load, pageContract),
      sourceParty: sourcePartyLabel(load),
      purpose: purposeLabel(detail, pageContract),
      expectedCount: load.expected_count,
      goatsInLoad: detail ? String((detail.goats ?? []).length) : copy(pageContract, "label.placeholder"),
      warmup: warmupCell(load, detail, pageContract),
      tagging: taggingLabel(detail, load.expected_count, pageContract),
      hfVaccination: hfVaccinationLabel(detail, pageContract),
      healthSelection: healthSelectionLabel(load.status, pageContract),
      status: {
        label: optionLabel(pageContract, "source_load_status", load.status),
        tone: contractTone(pageContract, "source_load_status", load.status),
      },
      detailHref: hrefWithQuery(`/procurement/source-entry/loads/${encodeURIComponent(load.load_id)}`, sp, {
        source_load: null,
        cursor: null,
        cursor_stack: null,
        page: null,
      }),
    };
  });

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
	            <b>{copy(pageContract, "crumb")}</b> · {pageContract.title}
	          </div>
	          <h1>{pageContract.title}</h1>
	          <div className="sub">{pageContract.subtitle}</div>
        </div>
        <div className="sp" style={{ flex: 1 }} />
      </div>

      {/* Journey IA — the full source-entry chain, so park arrival reads as one late gate, not the start. */}
	      <div className="fchipsbar" style={{ marginBottom: 14, flexWrap: "wrap" }} aria-label={copy(pageContract, "section.journey.aria")}>
	        <Truck className="ic" style={{ width: 14, color: "var(--brand-d)" }} aria-hidden="true" />
	        {journeyStages.map((stage, i) => (
	          <span key={stage.key} style={{ display: "inline-flex", alignItems: "center", gap: 8 }}>
	            <span className="muted small">{stage.label}</span>
	            {i < journeyStages.length - 1 ? <ArrowRight className="ic" style={{ width: 12, opacity: 0.5 }} aria-hidden="true" /> : null}
	          </span>
	        ))}
      </div>

      {actionStatus ? (
        actionStatus === "success" ? (
	          <div className="note" style={{ marginBottom: 14 }}>
	            <Tag tone="ok">{copy(pageContract, "action.success_tag")}</Tag> {actionFeedbackCopy(pageContract, actionStatus, actionKey)}
          </div>
        ) : (
          <div className="alert" style={{ marginBottom: 14 }}>
	            <b>{copy(pageContract, "action.failed_title")}</b>&nbsp;{actionFeedbackCopy(pageContract, actionStatus, actionKey)}
          </div>
        )
      ) : null}

      <NewLoadForm returnTo={hrefWithQuery(pathname, sp, { source_load: null })} pageContract={pageContract} />

      {!result.ok ? (
        <div className="alert" style={{ marginBottom: 14 }}>
          <b>{result.error.code ?? result.error.kind}</b>&nbsp;{result.error.message}
        </div>
      ) : null}

      {/* Status filter (server-side ?status). Park/date scope stays in the top bar; this is a page control. */}
      <div className="chipset" style={{ marginBottom: 14 }}>
        <Link href={statusHref("all")} replace scroll={false} className={`chip${statusFilter === "all" ? " on" : ""}`}>
	          {copy(pageContract, "filter.all_states")}
        </Link>
        {sourceLoadStatuses.map((status) => (
          <Link key={status.key} href={statusHref(status.key as ProcurementLoadStatus)} replace scroll={false} className={`chip${statusFilter === status.key ? " on" : ""}`}>
            {status.label}
          </Link>
        ))}
      </div>

      <section className="card">
        <div className="hd">
          <PackageSearch className="ic" style={{ color: "var(--info)" }} aria-hidden="true" />
	          <h3>{copy(pageContract, "section.loads.title")}</h3>
	          <Tag tone={loads.length ? "info" : "mut"}>{copy(pageContract, "section.loads.badge")}</Tag>
	          <div className="sp" style={{ flex: 1 }} />
	          <span className="muted small">{copy(pageContract, "section.loads.note")}</span>
        </div>
        <div className="tbar">
	          <VisibleTableSearch pageContract={pageContract} label={copy(pageContract, "filter.search_label")} />
	          <VaccinationFilterButton
	            pageContract={pageContract}
	            title={copy(pageContract, "filter.drawer.title")}
	            searchReason={copy(pageContract, "filter.search_reason")}
	            filterReason={copy(pageContract, "filter.reason")}
	            rowsLabel={`${loads.length} ${copy(pageContract, "label.rows")} · ${copy(pageContract, "filter.rows_suffix")}`}
	            facets={loadLabels}
	          />
	          <span className="muted small">{loads.length} {copy(pageContract, "label.rows")}</span>
	          <span className="muted small">{copy(pageContract, "section.loads.row_hint")}</span>
	        </div>
	        <div style={{ overflowX: "auto" }} tabIndex={0} role="group" aria-label={copy(pageContract, "section.loads.aria")}>
          <table className="source-loads-table">
            <thead>
              <tr>
	                {loadLabels.map((c) => (
                  <th key={c}>{c}</th>
                ))}
              </tr>
            </thead>
            <tbody>
              {loads.length === 0 ? (
                <tr>
	                  <td colSpan={loadLabels.length}>
                    <div className="muted small" style={{ padding: "18px 4px", textAlign: "center", lineHeight: 1.6 }}>
	                      {result.ok
	                        ? statusFilter === "all"
	                          ? copy(pageContract, "empty.loads_detail")
	                          : `${copy(pageContract, "empty.loads_filtered_prefix")} “${optionLabel(pageContract, "source_load_status", statusFilter as ProcurementLoadStatus)}” ${copy(pageContract, "empty.loads_filtered_suffix")}`
	                        : copy(pageContract, "empty.unavailable")}
                    </div>
                  </td>
                </tr>
              ) : (
                loads.map((load) => {
                  const drawerHref = hrefWithQuery(pathname, sp, { source_load: load.load_id });
                  const healthSelection = healthSelectionLabel(load.status, pageContract);
                  const detail = detailByLoad.get(load.load_id);
                  const hfVaccination = hfVaccinationLabel(detail, pageContract);
                  const purpose = purposeLabel(detail, pageContract);
                  const warmup = warmupCell(load, detail, pageContract);
                  return (
                    <tr key={load.load_id}>
                      <td>
                        <LocalOverlayLink href={drawerHref} className="celllink" scroll={false}>
                          <span className="gid">{shortId(load.load_id)}</span>
                        </LocalOverlayLink>
                      </td>
                      <td>
                        <LocalOverlayLink href={drawerHref} className="celllink" scroll={false}>
                          <b>{sourceLocationLabel(load, pageContract)}</b>
                          <div className="muted small">{copy(pageContract, "label.supplier_prefix")} {sourcePartyLabel(load)}</div>
                        </LocalOverlayLink>
                      </td>
                      <td>
                        <LocalOverlayLink href={drawerHref} className="celllink" scroll={false}>
                          <Tag tone={purpose === copy(pageContract, "label.placeholder") || purpose === optionLabel(pageContract, "proc_purpose", "fattening") ? "mut" : "ok"}>{purpose}</Tag>
                        </LocalOverlayLink>
                      </td>
                      <td>
                        <LocalOverlayLink href={drawerHref} className="celllink" scroll={false}>
                          {load.expected_count}
                        </LocalOverlayLink>
                      </td>
                      <td>
                        <LocalOverlayLink href={drawerHref} className="celllink" scroll={false}>
                          <Tag tone={warmup.tone}>{warmup.label}</Tag>
                          <div className="muted small" title={warmup.note}>
                            {load.purchase_date ? `${copy(pageContract, "label.from_date_prefix")} ${fmtDate(load.purchase_date)}` : copy(pageContract, "label.purchase_date_missing")}
                          </div>
                        </LocalOverlayLink>
                      </td>
                      <td>
                        <LocalOverlayLink href={drawerHref} className="celllink" scroll={false}>
                          <Tag tone="mut">{taggingLabel(detail, load.expected_count, pageContract)}</Tag>
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
                          <Tag tone={contractTone(pageContract, "source_load_status", load.status)}>{optionLabel(pageContract, "source_load_status", load.status)}</Tag>
                          <ArrowRight className="ic" style={{ width: 13, flexShrink: 0, marginLeft: 6 }} aria-hidden="true" />
                        </LocalOverlayLink>
                      </td>
                    </tr>
                  );
                })
              )}
            </tbody>
          </table>
        </div>
        <ProcurementPager prevHref={prevHref} nextHref={nextHref} page={page} count={loads.length} noun={loadLabels[0].toLowerCase()} forceVisible />
      </section>
      <SourceEntryLocalDrawer
        items={drawerItems}
        initialSelectedId={selectedLoadId}
        closeHref={closeDrawerHref}
        loadLabels={loadLabels}
        pageContract={pageContract}
      />
    </div>
  );
}
