import Link from "@/components/no-prefetch-link";
import { LocalOverlayLink } from "@/components/local-overlay-link";
import { redirect } from "next/navigation";
import { Syringe } from "lucide-react";
import { getVaccinationAdherence } from "@/lib/api/server";
import type { AdherenceRow, ProcessIntegritySeverity, WorkState } from "@/lib/api/server";
import { copy, optionalCopy, optionGroup, optionLabel, optionTone, tableLabels, tablePageSizes, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import { boundedInt, hrefPreviousPagedCursor, hrefWithPagedCursor, one, type RouteSearchParams } from "@/lib/search-params";
import { backendScope, parseScope, scopeHref } from "@/lib/scope";
import { SEVERITY_ORDER, WORK_STATE_ORDER, type Tone } from "./process-integrity";
import { ClipText, Tag } from "@/components/ui-primitives";
import { VaccinationFilterButton, VaccinationTablePager, type VaccinationPageSize } from "@/features/preventive-care-vaccination";
import { vaccinationDriveDisplayName } from "@/lib/vaccine-display";
import { ProtocolAdherenceLocalDrawer, type ProtocolAdherenceDrawerRecord } from "./protocol-adherence-local-drawer";
import { fmtDate } from "@/lib/format";
import { EvidenceMedia } from "./evidence-media";

type Tone4 = "ok" | "warn" | "dng" | "info" | "mut";
const accentVar: Record<Tone4, string> = {
  ok: "var(--ok)",
  warn: "var(--amber)",
  dng: "var(--danger)",
  info: "var(--info)",
  mut: "var(--line)",
};

function Kpi({ label, value, sub, tone = "mut" }: { label: string; value: React.ReactNode; sub?: string; tone?: Tone4 }) {
  return (
    <div className="kpi">
      <span className="acc" style={{ background: accentVar[tone] }} />
      <div className="lab">{label}</div>
      <div className="val">{value}</div>
      {sub ? <div className="dl muted">{sub}</div> : null}
    </div>
  );
}

function ownerOf(pageContract: AdminUiPageContract, row: AdherenceRow): string {
  return row.owner?.operator_name ?? row.owner?.park_head_name ?? copy(pageContract, "label.unassigned");
}

function adherenceSubtitle(pageContract: AdminUiPageContract): string {
  return pageContract.subtitle.replace("evidence, owner, and next action", "evidence, owner chain, and next action");
}

function adherenceLedgerLabels(pageContract: AdminUiPageContract): string[] {
  const labels = [...tableLabels(pageContract, "adherence-ledger")];
  if ((labels[4] || "").toLowerCase() === "owner") {
    labels[4] = `${labels[4]} chain`.toUpperCase();
  }
  if ((labels[4] || "").toLowerCase() === "owner chain") {
    labels[4] = copy(pageContract, "label.owner_short");
  }
  return labels;
}

function AdherenceInfo({ pageContract }: { pageContract: AdminUiPageContract }) {
  return (
    <details className="metric-help">
      <summary aria-label={copy(pageContract, "adherence.help.aria")}>{copy(pageContract, "label.info_icon")}</summary>
      <div className="metric-help-panel" role="note">
        <b>{copy(pageContract, "adherence.help.title")}</b>
        <span>{copy(pageContract, "adherence.help.window_prefix")}</span>
        <span>{copy(pageContract, "adherence.help.formula")}</span>
        <span>{copy(pageContract, "adherence.help.current_prefix")}</span>
      </div>
    </details>
  );
}

function copyOr(pageContract: AdminUiPageContract, key: string, fallback: string): string {
  return optionalCopy(pageContract, key) ?? fallback;
}

function workStateTone(pageContract: AdminUiPageContract, workState: string): Tone {
  return optionTone(pageContract, "work_state_filter_chips", workState) as Tone;
}

function gapLabel(pageContract: AdminUiPageContract, row: AdherenceRow): string {
  switch (row.drive_capacity_state) {
    case "over_cap_required":
      if (driveCapacityWithinSlots(row)) return copyOr(pageContract, "gap.none", "none");
      return copyOr(pageContract, "gap.capacity_shortfall", "capacity shortfall");
    case "medical_defer":
      return row.drive_medical_defer_reason ? `medical defer: ${row.drive_medical_defer_reason}` : "medical defer";
    case "terminal_animal_closed":
      return row.drive_medical_defer_reason ? `terminal closed: ${row.drive_medical_defer_reason}` : "terminal animal closed";
  }
  switch (row.gap) {
    case "proof_missing":
      return copyOr(pageContract, "gap.proof_missing", "proof missing");
    case "verification_pending":
      return copyOr(pageContract, "gap.verification_pending", "verification pending");
    case "deferred_explained":
      return copyOr(pageContract, "gap.deferred_explained", "deferred / explained");
    case "proof_rejected":
      return copyOr(pageContract, "gap.proof_rejected", "proof rejected");
    case "missed":
      return copyOr(pageContract, "gap.missed", "missed");
    case "blocked":
      return copyOr(pageContract, "gap.blocked", "blocked");
    case "overdue":
      return copyOr(pageContract, "gap.overdue", "overdue");
    default:
      return row.gap.replaceAll("_", " ");
  }
}

function driveCapacityWithinSlots(row: AdherenceRow): boolean {
  const slots = (row.drive_available_operators ?? 0) * (row.drive_operator_cap ?? 0);
  const animals = row.drive_animals_assigned ?? row.drive_animals_required ?? 0;
  return slots > 0 && animals <= slots;
}

function driveCapacityDetail(row: AdherenceRow): string | null {
  if (row.drive_capacity_state !== "over_cap_required") return null;
  const slots = (row.drive_available_operators ?? 0) * (row.drive_operator_cap ?? 0);
  const animals = row.drive_animals_assigned ?? row.drive_animals_required ?? 0;
  const latest = row.drive_latest_safe_date ? fmtDate(row.drive_latest_safe_date) : undefined;
  if (driveCapacityWithinSlots(row)) {
    return latest
      ? `${animals.toLocaleString("en-IN")} assigned / ${slots.toLocaleString("en-IN")} slots · latest safe ${latest}`
      : `${animals.toLocaleString("en-IN")} assigned / ${slots.toLocaleString("en-IN")} slots`;
  }
  const shortage = slots > 0 ? animals - slots : animals;
  return `${shortage.toLocaleString("en-IN")} more animal${shortage === 1 ? "" : "s"} than planned capacity${latest ? ` · latest safe ${latest}` : ""}`;
}

const VACCINE_CODE_COPY_KEYS: Array<[needle: string, copyKey: string]> = [
  ["blue_tongue", "vaccine.blue_tongue"],
  ["goat_pox", "vaccine.goat_pox"],
  ["sheep_pox", "vaccine.sheep_pox"],
  ["et_tt", "vaccine.et_tt"],
  ["fmd", "vaccine.fmd"],
  ["ppr", "vaccine.ppr"],
  ["hs", "vaccine.hs"],
];

function readableAdherenceExpected(pageContract: AdminUiPageContract, raw: string): { title: string; detail: string } {
  const withoutPrefix = raw.replace(/^Preventive Care Vaccination Matrix\s*/i, "").trim();
  const code = withoutPrefix.match(/[a-z0-9]+(?:_[a-z0-9]+)+/i)?.[0]?.toLowerCase() ?? "";
  const vaccineCopyKey = VACCINE_CODE_COPY_KEYS.find(([needle]) => code.includes(needle))?.[1] ?? "vaccine.generic";
  const vaccine = copy(pageContract, vaccineCopyKey);
  const path = code.includes("_kid_")
    ? copy(pageContract, "schedule.kid_course")
    : code.includes("_adult_")
      ? copy(pageContract, "schedule.adult_course")
      : copy(pageContract, "schedule.course");
  const timing = readableScheduleTiming(pageContract, code);
  const dueCount = raw.match(/:\s*(\d+)\s*(?:animals\s+must\s+finish|due|d\b)/i)?.[1];
  const sharedLabel = vaccinationDriveDisplayName(withoutPrefix);
  const fallbackLabel = copy(pageContract, "label.vaccination_drive");
  const noisySharedLabel = /^Preventive Care Vaccination Matrix\b/i.test(sharedLabel);
  const bits = sharedLabel && sharedLabel !== fallbackLabel && !noisySharedLabel ? [sharedLabel] : [vaccine, path, timing].filter(Boolean);
  return {
    title: `${bits.join(" ")}${dueCount ? ` - ${dueCount} ${copy(pageContract, "label.due_lower")}` : ""}`,
    detail: bits.join(" "),
  };
}

function readableAdherenceActual(pageContract: AdminUiPageContract, raw: string): string {
  const text = raw.trim();
  const deferred = text.match(/^(\d+)\s+deferred\/ex/i);
  if (deferred) return `${deferred[1]} ${copy(pageContract, "actual.deferred_with_reason")}`;
  if (text.toLowerCase() === "not completed") return copy(pageContract, "actual.not_completed_yet");
  return text.replaceAll("_", " ");
}

function readableScheduleTiming(pageContract: AdminUiPageContract, code: string): string | undefined {
  const match = code.match(/_(\d+)(w|m|yr)$/);
  if (!match) return undefined;
  const [, value, unit] = match;
  const unitKey = unit === "w" ? "schedule.weeks" : unit === "m" ? "schedule.months" : "schedule.years";
  return `${value} ${copy(pageContract, unitKey)}`;
}

export async function ProtocolAdherencePage({
  searchParams,
  pageContract,
}: {
  searchParams?: RouteSearchParams;
  pageContract: AdminUiPageContract;
}) {
  const sp = searchParams ?? {};
  const severityFilter = (SEVERITY_ORDER.find((s) => s === one(sp, "severity")) ?? "all") as ProcessIntegritySeverity | "all";
  const workStateOptions = optionGroup(pageContract, "work_state_filter_chips");
  const workStateParam = one(sp, "state");
  const workStateFilter = (workStateOptions.some((option) => option.key === workStateParam) ? workStateParam : "all") as WorkState | "all";
  const scope = parseScope(sp);
  const { parkId, asOf } = backendScope(scope);
  const pageSizeOptions = tablePageSizes(pageContract, "adherence-ledger");
  const PATH = "/protocol-adherence";
  const requestedPageSize = (pageSizeOptions.find((size) => size === boundedInt(one(sp, "adh_limit"), 10, 1, 100)) ?? 10) as VaccinationPageSize;
  const adhCursor = one(sp, "adh_cursor");
  const adhCursorStack = sp.adh_cursor_stack;
  const adhPage = boundedInt(one(sp, "adh_page"), 1, 1, 1000000);

  const result = await getVaccinationAdherence({
    parkId,
    asOf,
    workState: workStateFilter === "all" ? undefined : workStateFilter,
    severity: severityFilter === "all" ? undefined : severityFilter,
    limit: requestedPageSize,
    cursor: adhCursor,
  });

  const summary = result.ok ? result.data.summary : null;
  const rows: AdherenceRow[] = result.ok ? result.data.rows : [];
  const hasLedgerFilters = severityFilter !== "all" || workStateFilter !== "all";
  const totalCount = result.ok ? result.data.total_count : 0;
  const nextCursor = result.ok ? result.data.next_cursor : undefined;
  const start = totalCount === 0 ? 0 : (adhPage - 1) * requestedPageSize + 1;
  const end = totalCount === 0 ? 0 : Math.min(totalCount, start + rows.length - 1);
  const paged = { items: rows, page: adhPage, pageSize: requestedPageSize, total: totalCount, start, end };
  const nextHref = nextCursor ? hrefWithPagedCursor(PATH, sp, "adh_cursor", nextCursor, "adh_page", "adh_cursor_stack") : null;
  const prevHref = hrefPreviousPagedCursor(PATH, sp, "adh_cursor", "adh_page", "adh_cursor_stack");
  if (result.ok && adhPage > 1 && !adhCursor && !adhCursorStack) {
    redirect(scopeHref("/protocol-adherence", scope, {}, {
      severity: severityFilter,
      state: workStateFilter,
      adh_page: "1",
      adh_limit: String(requestedPageSize),
    }));
  }
  const ledgerLabels = adherenceLedgerLabels(pageContract);
  const initialSelectedRowId = one(sp, "adh_row");

  // Filter links preserve the full top-bar scope (scopeHref) + the page severity filter.
  // Filter/page-size changes reset to page 1 and drop the cursor stack (keyset restart).
  function hrefWith(overrides: Record<string, string | undefined>): string {
    return scopeHref("/protocol-adherence", scope, {}, {
      severity: severityFilter,
      state: workStateFilter,
      adh_page: String(paged.page),
      adh_limit: String(paged.pageSize),
      adh_cursor: undefined,
      adh_cursor_stack: undefined,
      ...overrides,
    });
  }
  function pagerHref(page: number): string {
    if (page > paged.page) return nextHref ?? hrefWith({});
    if (page < paged.page) return prevHref ?? hrefWith({});
    return hrefWith({ adh_page: String(page) });
  }
  function pageSizeHref(pageSize: VaccinationPageSize): string {
    return hrefWith({ adh_page: "1", adh_limit: String(pageSize), adh_cursor: undefined, adh_cursor_stack: undefined });
  }
  const closeDrawerHref = hrefWith({ adh_row: undefined });
  const rowDrawerHref = (row: AdherenceRow) => `${closeDrawerHref}#adh_row=${encodeURIComponent(row.row_id)}`;
  const workflowHref = (row: AdherenceRow) =>
    scopeHref(`/workflows/${encodeURIComponent(row.row_id)}`, scope, {}, { from: "protocol-adherence" });
  const drawerRecords: ProtocolAdherenceDrawerRecord[] = rows.map((row) => {
    const expected = readableAdherenceExpected(pageContract, row.expected);
    return {
      row,
      expectedTitle: expected.title,
      expectedDetail: expected.detail,
      actual: readableAdherenceActual(pageContract, row.actual),
      gap: gapLabel(pageContract, row),
      owner: ownerOf(pageContract, row),
      workflowHref: workflowHref(row),
      actionCenterHref: scopeHref("/action-center", scope, {}, { ac_row: row.row_id }),
    };
  });

  return (
    <div className="screen on">
	      <div className="phead">
	        <div>
	          <div className="title-with-help">
	            <h1>{pageContract.title}</h1>
	            <AdherenceInfo pageContract={pageContract} />
	          </div>
	          <div className="sub">{adherenceSubtitle(pageContract)}</div>
	        </div>
	      </div>

      {/* Mock KPI row, from the real adherence summary. Park/date scope lives in the top bar only. */}
      <div className="grid g4" style={{ marginBottom: 14 }}>
        <Kpi
	          label={copy(pageContract, "label.overall_adherence")}
	          value={summary ? `${Math.round(summary.adherence_percent)}%` : "n/a"}
	          sub={copy(pageContract, "label.on_time_correct")}
	          tone={summary ? (summary.adherence_percent >= 90 ? "ok" : summary.adherence_percent >= 70 ? "warn" : "dng") : "mut"}
	        />
	        <Kpi label={copy(pageContract, "label.open_process_gaps")} value={summary ? summary.open_gap_count : "n/a"} sub={copy(pageContract, "label.across_rules")} tone={summary && summary.open_gap_count > 0 ? "warn" : "mut"} />
	        <Kpi label={copy(pageContract, "label.deferred_explained")} value={summary ? summary.deferred_count : "n/a"} sub={copy(pageContract, "label.deferred_scope")} tone="mut" />
	        <Kpi label={copy(pageContract, "label.on_track")} value={summary ? summary.process_intact_count : "n/a"} sub={summary ? `${summary.completed_count}/${summary.expected_count} ${copy(pageContract, "label.done_suffix")}` : copy(pageContract, "label.obligations")} tone="ok" />
      </div>

      {!result.ok ? (
        <div className="alert" style={{ marginBottom: 14 }}>
          <b>{result.error.code ?? result.error.kind}</b>&nbsp;{result.error.message}
        </div>
      ) : null}

      {/* Severity + work-state filters (server-side). */}
	      <div className="chipset" style={{ marginBottom: 14 }}>
	        <Link href={hrefWith({ severity: "all", adh_page: "1" })} replace scroll={false} className={`chip${severityFilter === "all" ? " on" : ""}`}>
	          {copy(pageContract, "label.all_severity")}
	        </Link>
	        {SEVERITY_ORDER.map((s) => (
	          <Link key={s} href={hrefWith({ severity: s, adh_page: "1" })} replace scroll={false} className={`chip${severityFilter === s ? " on" : ""}`}>
	            {optionLabel(pageContract, "severity_chips", s)}
	          </Link>
	        ))}
      </div>
      <div className="chipset" style={{ marginBottom: 14 }}>
        <Link href={hrefWith({ state: "all", adh_page: "1" })} replace scroll={false} className={`chip${workStateFilter === "all" ? " on" : ""}`}>
          {copy(pageContract, "label.all_states")}
        </Link>
        {WORK_STATE_ORDER.map((state) => (
          <Link key={state} href={hrefWith({ state, adh_page: "1" })} replace scroll={false} className={`chip${workStateFilter === state ? " on" : ""}`}>
            {optionLabel(pageContract, "work_state_filter_chips", state)}
          </Link>
        ))}
      </div>

      <section className="card">
        <div className="hd">
          <Syringe className="ic" style={{ color: "var(--info)" }} aria-hidden="true" />
	          <h3>{copy(pageContract, "section.ledger.title")}</h3>
	          {summary ? <Tag tone={summary.adherence_percent >= 90 ? "ok" : "warn"}>{Math.round(summary.adherence_percent)}% adherence</Tag> : null}
	          <div className="sp" style={{ flex: 1 }} />
	          <span className="muted small">{copy(pageContract, "section.ledger.note")}</span>
	        </div>
	        <div className="tbar">
	          <VaccinationFilterButton
	            pageContract={pageContract}
	            title={copy(pageContract, "filter.drawer.title")}
	            searchReason={copy(pageContract, "filter.search_reason")}
	            filterReason={copy(pageContract, "filter.reason")}
	            rowsLabel={`${paged.start}-${paged.end} of ${paged.total} rows · ${copy(pageContract, "filter.rows_suffix")}`}
	            actionHref={scopeHref("/action-center", scope)}
	            actionLabel={copy(pageContract, "action.open_action_center")}
	            facets={ledgerLabels}
	          />
          <span className="muted small">
            {paged.start}-{paged.end} of {paged.total} rows
          </span>
	          <span className="muted small">{copy(pageContract, "filter.click_row")}</span>
	        </div>
	        <div style={{ overflowX: "auto" }} tabIndex={0} role="group" aria-label={copy(pageContract, "section.ledger.aria")}>
          <table className="table-fixed adherence-table">
            <colgroup>
              <col style={{ width: "27%" }} />
              <col style={{ width: "13%" }} />
              <col style={{ width: "19%" }} />
              <col style={{ width: "10%" }} />
              <col style={{ width: "11%" }} />
              <col style={{ width: "14%" }} />
              <col style={{ width: "6%" }} />
            </colgroup>
            <thead>
              <tr>
	                {ledgerLabels.map((c) => (
	                  <th key={c}>{c}</th>
                ))}
              </tr>
            </thead>
            <tbody>
              {paged.total === 0 ? (
                <tr>
	                  <td colSpan={ledgerLabels.length}>
                    <div className="muted small" style={{ padding: "18px 4px", textAlign: "center", lineHeight: 1.6 }}>
	                      {hasLedgerFilters
	                        ? copy(pageContract, "empty.ledger_filtered")
	                        : copy(pageContract, "empty.ledger_detail")}
                    </div>
                  </td>
                </tr>
              ) : (
                paged.items.map((row) => {
                  const href = rowDrawerHref(row);
                  const expected = readableAdherenceExpected(pageContract, row.expected);
                  const actual = readableAdherenceActual(pageContract, row.actual);
                  const driveDetail = driveCapacityDetail(row);
                  return (
                    <tr key={row.row_id}>
                      <td>
                        <LocalOverlayLink href={href} className="celllink" scroll={false} title={row.expected}>
                          <ClipText title={expected.title} className="strong">
                            {expected.title}
                          </ClipText>
                          <span className="mt">{expected.detail}</span>
                        </LocalOverlayLink>
                      </td>
                      <td className="muted">
                        <LocalOverlayLink href={href} className="celllink" scroll={false} title={row.actual}>
                          <ClipText title={actual}>{actual}</ClipText>
                        </LocalOverlayLink>
                      </td>
                      <td>
                        <LocalOverlayLink href={href} className="celllink" scroll={false}>
                          <Tag tone={workStateTone(pageContract, row.work_state)}>{gapLabel(pageContract, row)}</Tag>
                          {driveDetail ? <span className="mt gap-detail">{driveDetail}</span> : null}
                        </LocalOverlayLink>
                      </td>
                      <td>
                        <LocalOverlayLink href={href} className="celllink" scroll={false}>
	                          <Tag tone={optionTone(pageContract, "severity_chips", row.severity) as Tone}>{optionLabel(pageContract, "severity_chips", row.severity)}</Tag>
                        </LocalOverlayLink>
                      </td>
                      <td className="muted">
	                        <LocalOverlayLink href={href} className="celllink" scroll={false} title={ownerOf(pageContract, row)}>
	                          <ClipText title={ownerOf(pageContract, row)}>{ownerOf(pageContract, row)}</ClipText>
                        </LocalOverlayLink>
                      </td>
                      <td>
                        <LocalOverlayLink href={href} className="celllink" scroll={false} title={row.next_action}>
                          <ClipText title={row.next_action} className="lk small">
                            {row.next_action} →
                          </ClipText>
                        </LocalOverlayLink>
                      </td>
                      <td>
                        <LocalOverlayLink href={href} className="celllink" scroll={false}>
	                          <EvidenceMedia evidence={row.evidence} pageContract={pageContract} />
                        </LocalOverlayLink>
                      </td>
                    </tr>
                  );
                })
              )}
            </tbody>
          </table>
        </div>
        <VaccinationTablePager
          pageContract={pageContract}
          pageSizeOptions={pageSizeOptions}
          page={paged.page}
          pageSize={paged.pageSize}
          total={paged.total}
          start={paged.start}
          end={paged.end}
	          noun={copy(pageContract, "table.ledger.noun")}
          hrefForPage={pagerHref}
          hrefForPageSize={pageSizeHref}
        />
      </section>

      <div className="note" style={{ marginTop: 14 }}>
	        {copy(pageContract, "note.computation")}{" "}
	        <Link href="/config?category=vaccination" className="lk">
	          {copy(pageContract, "action.open_config")}
	        </Link>{" "}
	        {copy(pageContract, "note.computation.joiner")}{" "}
	        <Link href="/sops" className="lk">
	          {copy(pageContract, "action.open_sops")}
	        </Link>
	        {copy(pageContract, "note.computation.tail")}
      </div>

      <ProtocolAdherenceLocalDrawer
        records={drawerRecords}
        ledgerLabels={ledgerLabels}
        closeHref={closeDrawerHref}
        initialSelectedRowId={initialSelectedRowId}
        pageContract={pageContract}
      />
    </div>
  );
}
