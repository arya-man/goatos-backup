import Link from "@/components/no-prefetch-link";
import { LocalOverlayLink } from "@/components/local-overlay-link";
import { ArrowRight, Syringe } from "lucide-react";
import type { ActionCenterObligation, WorkState } from "@/lib/api/server";
import { copy, optionGroup, optionalOption, type AdminUiOption, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import {
  TONE_SWATCH,
  type Tone,
} from "./process-integrity";
import { Tag } from "@/components/ui-primitives";
import { fmtDate } from "@/lib/format";
import { operationalLocationLabel } from "@/lib/operational-location.ts";
import { actionDriveLabel, actionWorkTitle } from "./action-center-presenters";

export { actionDriveLabel, actionWorkTitle } from "./action-center-presenters";

// SLA tint for the due chip, mapped from the server-computed work state (no client date math).
function slaClass(state: WorkState): string {
  if (state === "overdue" || state === "rejected" || state === "blocked") return "brk";
  if (state === "due" || state === "proof_pending" || state === "verification_pending") return "run";
  if (state === "completed") return "ok";
  return "";
}

function initials(name?: string): string {
  if (!name) return "—";
  return name
    .split(/\s+/)
    .map((w) => w[0] ?? "")
    .join("")
    .slice(0, 2)
    .toUpperCase();
}

function displayBlocker(reason?: string | null): string | null {
  if (!reason) return null;
  return reason;
}

function driveCapacityTag(pageContract: AdminUiPageContract, row: ActionCenterObligation): { tone: Tone; label: string; title: string } | null {
  switch (row.drive_capacity_state) {
    case "over_cap_required": {
      const slots = (row.drive_available_operators ?? 0) * (row.drive_operator_cap ?? 0);
      const animals = row.drive_animals_assigned ?? row.drive_animals_required ?? 0;
      return {
        tone: "dng",
        label: copy(pageContract, "label.drive_over_cap_required"),
        title: copy(pageContract, "tooltip.drive_over_cap_required")
          .replace("{animals}", String(animals))
          .replace("{slots}", String(slots)),
      };
    }
    case "medical_defer":
      return {
        tone: "warn",
        label: copy(pageContract, "label.drive_medical_defer"),
        title: row.drive_medical_defer_reason ?? copy(pageContract, "tooltip.drive_medical_defer"),
      };
    case "terminal_animal_closed":
      return {
        tone: "mut",
        label: copy(pageContract, "label.drive_terminal_closed"),
        title: row.drive_medical_defer_reason ?? copy(pageContract, "tooltip.drive_terminal_closed"),
      };
    default:
      return null;
  }
}

function eventCode(row: ActionCenterObligation): string {
  return row.shed_name || row.dose_code;
}

function shortDueLabel(value: string): string {
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return fmtDate(value);
  return new Intl.DateTimeFormat("en-GB", { day: "2-digit", month: "short", timeZone: "Asia/Kolkata" }).format(date);
}

function optionLabel(options: AdminUiOption[], key: string): string {
  const option = options.find((item) => item.key === key);
  if (!option) throw new Error(`Admin-web contract missing option ${key}`);
  return option.label;
}

function optionTone(options: AdminUiOption[], key: string): Tone {
  const option = options.find((item) => item.key === key);
  if (!option) throw new Error(`Admin-web contract missing option ${key}`);
  return (option.tone || "mut") as Tone;
}

// One mock-shaped task card for a single Action Center obligation (ported from the mock taskCard2).
function WorkCard({ pageContract, row, href, localOverlay }: { pageContract: AdminUiPageContract; row: ActionCenterObligation; href: string; localOverlay: boolean }) {
  const operatorMissing = row.owner_state === "missing" || !row.owner?.operator_name;
  const blocker = displayBlocker(row.blocker_reason);
  const drive = actionDriveLabel(pageContract, row);
  const title = actionWorkTitle(pageContract, row);
  const ownerLabel = operatorMissing ? copy(pageContract, "label.owner_chain_assign") : row.owner?.operator_name;
  const progress = row.expected_count > 0 ? `${row.completed_count}/${row.expected_count} ${copy(pageContract, "label.done_suffix")}` : null;
  const showBlocker = blocker && !operatorMissing;
  const fallbackEvent = copy(pageContract, "label.vaccination");
  const shedDisplay = row.operational_location_display || operationalLocationLabel({ shedName: row.shed_name, partitionLabel: row.partition_label });
  const openLabel = `${copy(pageContract, "label.open_work_item_for")} ${shedDisplay || copy(pageContract, "label.shed_fallback")}`;
  const parkDisplay = optionalOption(pageContract, "park_display_chips", row.park_id);
  const parkLabel = parkDisplay?.label || row.park_name || row.park_id;
  const parkTone = (parkDisplay?.tone || "info") as Tone;
  const severityOptions = optionGroup(pageContract, "severity_chips");
  const workStateOptions = optionGroup(pageContract, "work_state_filter_chips");
  const proofStateOptions = optionGroup(pageContract, "proof_state_chips");
  const driveTag = driveCapacityTag(pageContract, row);
  const contents = (
    <>
      <div className="tt">
        <span className="fic">
          <Syringe className="ic" style={{ width: 14 }} aria-hidden="true" />
        </span>
        <span className="ec" title={eventCode(row) || fallbackEvent}>{eventCode(row) || fallbackEvent}</span>
        <span className={`sla ${slaClass(row.work_state)}`} style={{ marginLeft: "auto" }} title={`${copy(pageContract, "label.due_prefix")} ${fmtDate(row.due_at)}`}>
          {copy(pageContract, "label.due_prefix")} {shortDueLabel(row.due_at)}
        </span>
      </div>
      <h4 title={title}>{title}</h4>
      <div className="muted small ac-drive" title={drive}>
        {drive}
      </div>
      <div className="row">
        <Tag tone="mut">{copy(pageContract, "label.vaccination")}</Tag>
        <Tag tone={parkTone}>{parkLabel}</Tag>
        <Tag tone={optionTone(severityOptions, row.severity)}>{optionLabel(severityOptions, row.severity)}</Tag>
      </div>
      <div className="row" style={{ marginTop: 6 }}>
        <Tag tone="info">{row.animal_stage}</Tag>
        <Tag tone={optionTone(workStateOptions, row.work_state)}>{optionLabel(workStateOptions, row.work_state)}</Tag>
        {row.proof_state !== "missing" ? <Tag tone={optionTone(proofStateOptions, row.proof_state)}>{optionLabel(proofStateOptions, row.proof_state)}</Tag> : null}
        {driveTag ? <Tag tone={driveTag.tone} title={driveTag.title}>{driveTag.label}</Tag> : null}
        {progress ? <span className="muted small ac-progress">{progress}</span> : null}
      </div>
      {row.drive_capacity_state === "over_cap_required" ? (
        <div className="muted small ac-progress" title={driveTag?.title}>
          {(row.drive_animals_assigned ?? row.drive_animals_required ?? 0).toLocaleString("en-IN")} animals · {(row.drive_available_operators ?? 0).toLocaleString("en-IN")} ops × {(row.drive_operator_cap ?? 0).toLocaleString("en-IN")}
        </div>
      ) : null}
      {showBlocker ? (
        <div className="ac-blocker" title={blocker}>
          {blocker.split(" - ")[0]}
        </div>
      ) : null}
      <div className="who">
        <span className="av xs">{initials(row.owner?.operator_name)}</span>
        <span className="cliptext" title={ownerLabel} style={operatorMissing ? { color: "var(--danger)" } : undefined} data-truncate>
          {ownerLabel}
        </span>
        <ArrowRight className="ic" style={{ width: 13, marginLeft: "auto", flexShrink: 0 }} aria-hidden="true" />
      </div>
    </>
  );
  const linkProps = {
    href,
    scroll: false,
    className: "task task-ac",
    "data-filter-row": true,
    "aria-label": openLabel,
    title: `${title} · ${drive} · ${ownerLabel ?? copy(pageContract, "label.unassigned")}`,
    style: { color: "inherit", textDecoration: "none" },
  } as const;
  return localOverlay ? (
    <LocalOverlayLink {...linkProps}>{contents}</LocalOverlayLink>
  ) : (
    <Link {...linkProps}>{contents}</Link>
  );
}

type BoardColumn = {
  key: string;
  label: string;
  tone: Tone;
  states: WorkState[];
};

// The mock Action Center is a six-lane status board. Vaccination has richer backend work states, so the
// visual lanes stay mock-shaped while each card still carries the exact server-computed work state.
const BOARD_COLUMN_STATES: Record<string, WorkState[]> = {
  pending: ["due", "scheduled", "proof_pending", "verification_pending", "in_progress"],
  ontime: ["completed"],
  late: ["overdue", "missed"],
  skipped: ["deferred"],
  deviated: ["blocked", "rejected"],
};

function boardColumns(pageContract: AdminUiPageContract): BoardColumn[] {
  return optionGroup(pageContract, "work_state_board_columns").map((option) => ({
    key: option.key,
    label: option.label,
    tone: (option.tone || "mut") as Tone,
    states: BOARD_COLUMN_STATES[option.key] ?? [],
  }));
}

export function boardWorkStates(pageContract: AdminUiPageContract): WorkState[] {
  const states: WorkState[] = [];
  for (const column of boardColumns(pageContract)) {
    for (const state of column.states) {
      if (!states.includes(state)) states.push(state);
    }
  }
  return states;
}

function boardColumnFor(row: ActionCenterObligation, columns: BoardColumn[]): BoardColumn {
  return columns.find((column) => column.states.includes(row.work_state)) ?? columns[0];
}

function columnCount(column: BoardColumn, stateCounts?: ReadonlyMap<WorkState, number>): number | undefined {
  if (!stateCounts) return undefined;
  return column.states.reduce((sum, state) => sum + (stateCounts.get(state) ?? 0), 0);
}

// Mock-shaped status board (ported from the mock taskboard): one .tcol per visual lane, header swatch +
// label + count, cards inside .tcards, and a "—" placeholder for empty columns. Source rows are the real
// ActionCenterResponse items — server-computed work state, shown inside each card.
export function WorkBoard({
  pageContract,
  rows,
  stateCounts,
  showAllColumns = true,
  drawerHrefForRow,
}: {
  pageContract: AdminUiPageContract;
  rows: ActionCenterObligation[];
  stateCounts?: ReadonlyMap<WorkState, number>;
  showAllColumns?: boolean;
  drawerHrefForRow?: (row: ActionCenterObligation) => string;
}) {
  const contractColumns = boardColumns(pageContract);
  const byColumn = new Map<string, ActionCenterObligation[]>();
  for (const r of rows) {
    const column = boardColumnFor(r, contractColumns);
    const list = byColumn.get(column.key) ?? [];
    list.push(r);
    byColumn.set(column.key, list);
  }
  const columns = showAllColumns ? contractColumns : contractColumns.filter((column) => (byColumn.get(column.key)?.length ?? 0) > 0);

  return (
    <div className="taskboard" role="group" aria-label={copy(pageContract, "section.work_board.aria")} tabIndex={0}>
      {columns.map((column) => {
        const col = byColumn.get(column.key) ?? [];
        const count = columnCount(column, stateCounts) ?? col.length;
        return (
          <div className="tcol" data-st={column.key} key={column.key}>
            <div className="tcolh">
              <span className="sw" style={{ background: TONE_SWATCH[column.tone] }} />
              {column.label}
              <span className="n">{count}</span>
            </div>
            <div className="tcards">
              {col.length ? (
                col.map((row) => (
                  <WorkCard
                    key={row.row_id}
                    pageContract={pageContract}
                    row={row}
                    href={drawerHrefForRow ? drawerHrefForRow(row) : `/workflows/${encodeURIComponent(row.row_id)}`}
                    localOverlay={Boolean(drawerHrefForRow)}
                  />
                ))
              ) : (
                <div className="muted small" style={{ padding: 10, textAlign: "center" }}>
                  {copy(pageContract, "label.empty_placeholder")}
                </div>
              )}
            </div>
          </div>
        );
      })}
    </div>
  );
}
