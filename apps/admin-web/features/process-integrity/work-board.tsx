import Link from "next/link";
import { ArrowRight, Syringe } from "lucide-react";
import type { ActionCenterObligation, WorkState } from "@/lib/api/server";
import {
  PROOF_META,
  SEVERITY_META,
  TONE_SWATCH,
  WORK_STATE_META,
  type Tone,
} from "./process-integrity";
import { Tag } from "@/components/ui-primitives";
import { fmtDate } from "@/lib/format";

// SLA tint for the due chip, mapped from the server-computed work state (no client date math).
function slaClass(state: WorkState): string {
  if (state === "overdue" || state === "rejected" || state === "blocked" || state === "owner_missing") return "brk";
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

function eventCode(row: ActionCenterObligation): string {
  return row.shed_name || row.dose_code || "Vaccination";
}

function shortDueLabel(value: string): string {
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return fmtDate(value);
  return new Intl.DateTimeFormat("en-GB", { day: "2-digit", month: "short", timeZone: "Asia/Kolkata" }).format(date);
}

function parkChipLabel(value: string): string {
  return value.toLowerCase() === "coimbatore" ? "CBE" : value;
}

export function actionDriveLabel(row: Pick<ActionCenterObligation, "drive_name" | "protocol_name" | "dose_code">): string {
  const raw = row.drive_name || `${row.protocol_name} ${row.dose_code}` || "Vaccination drive";
  const cleaned = raw
    .replace(/\s+[-–]\s+PHC-[A-Z0-9-]+$/i, "")
    .replace(/\s+[-–]\s+[A-Z]+-[A-Z0-9-]+$/i, "")
    .replace(/\s+/g, " ")
    .trim();
  return cleaned || "Vaccination drive";
}

export function actionWorkTitle(row: ActionCenterObligation): string {
  const shed = row.shed_name || "shed";
  if (row.work_state === "owner_missing" || row.owner_state === "missing" || !row.owner?.operator_name) {
    return `Assign owner chain — ${shed}`;
  }
  if (row.proof_state === "missing") return `Capture vaccination proof — ${shed}`;
  if (row.verification_state === "pending") return `Verify vaccination proof — ${shed}`;
  if (row.work_state === "overdue") return `${actionDriveLabel(row)} overdue — ${shed}`;
  return `${actionDriveLabel(row)} — ${shed}`;
}

// One mock-shaped task card for a single Action Center obligation (ported from the mock taskCard2).
function WorkCard({ row, href }: { row: ActionCenterObligation; href: string }) {
  const ownerMissing = row.owner_state === "missing" || !row.owner?.operator_name;
  const blocker = displayBlocker(row.blocker_reason);
  const drive = actionDriveLabel(row);
  const title = actionWorkTitle(row);
  const ownerLabel = ownerMissing ? "owner chain: assign" : row.owner?.operator_name;
  const progress = row.expected_count > 0 ? `${row.completed_count}/${row.expected_count} done` : null;
  const showBlocker = blocker && !ownerMissing;
  return (
    <Link
      href={href}
      scroll={false}
      className="task task-ac"
      aria-label={`Open Action Center work item for ${row.shed_name}`}
      style={{ color: "inherit", textDecoration: "none" }}
    >
      <div className="tt">
        <span className="fic">
          <Syringe className="ic" style={{ width: 14 }} aria-hidden="true" />
        </span>
        <span className="ec">{eventCode(row)}</span>
        <span className={`sla ${slaClass(row.work_state)}`} style={{ marginLeft: "auto" }} title={`due ${fmtDate(row.due_at)}`}>
          due {shortDueLabel(row.due_at)}
        </span>
      </div>
      <h4>{title}</h4>
      <div className="muted small ac-drive">
        {drive}
      </div>
      <div className="row">
        <Tag tone="mut">Vaccination</Tag>
        <Tag tone="info">{parkChipLabel(row.park_name)}</Tag>
        <Tag tone={SEVERITY_META[row.severity].tone}>{SEVERITY_META[row.severity].label}</Tag>
      </div>
      <div className="row" style={{ marginTop: 6 }}>
        <Tag tone="info">{row.animal_stage}</Tag>
        <Tag tone={WORK_STATE_META[row.work_state].tone}>{WORK_STATE_META[row.work_state].label}</Tag>
        {row.proof_state !== "missing" ? <Tag tone={PROOF_META[row.proof_state].tone}>{PROOF_META[row.proof_state].label}</Tag> : null}
        {progress ? <span className="muted small ac-progress">{progress}</span> : null}
      </div>
      {showBlocker ? (
        <div className="ac-blocker" title={blocker}>
          {blocker.split(" - ")[0]}
        </div>
      ) : null}
      <div className="who">
        <span className="av xs">{initials(row.owner?.operator_name)}</span>
        <span style={ownerMissing ? { color: "var(--danger)" } : undefined}>{ownerLabel}</span>
        <ArrowRight className="ic" style={{ width: 13, marginLeft: "auto", flexShrink: 0 }} aria-hidden="true" />
      </div>
    </Link>
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
const BOARD_COLUMNS: BoardColumn[] = [
  {
    key: "pending",
    label: "Pending",
    tone: "info",
    states: ["owner_missing", "due", "scheduled", "proof_pending", "verification_pending", "in_progress"],
  },
  { key: "ontime", label: "On-time", tone: "ok", states: ["completed"] },
  { key: "late", label: "Late", tone: "warn", states: ["overdue"] },
  { key: "skipped", label: "Skipped — silent", tone: "dng", states: ["rejected", "deferred"] },
  { key: "deviated", label: "Deviated", tone: "pur", states: ["blocked"] },
];

function boardColumnFor(row: ActionCenterObligation): BoardColumn {
  return BOARD_COLUMNS.find((column) => column.states.includes(row.work_state)) ?? BOARD_COLUMNS[0];
}

// Mock-shaped status board (ported from the mock taskboard): one .tcol per visual lane, header swatch +
// label + count, cards inside .tcards, and a "—" placeholder for empty columns. Source rows are the real
// ActionCenterResponse items — server-computed work state, shown inside each card.
export function WorkBoard({
  rows,
  showAllColumns = false,
  drawerHrefForRow,
}: {
  rows: ActionCenterObligation[];
  showAllColumns?: boolean;
  drawerHrefForRow?: (row: ActionCenterObligation) => string;
}) {
  const byColumn = new Map<string, ActionCenterObligation[]>();
  for (const r of rows) {
    const column = boardColumnFor(r);
    const list = byColumn.get(column.key) ?? [];
    list.push(r);
    byColumn.set(column.key, list);
  }
  const columns = showAllColumns ? BOARD_COLUMNS : BOARD_COLUMNS.filter((column) => (byColumn.get(column.key)?.length ?? 0) > 0);

  return (
    <div className="taskboard" role="group" aria-label="Vaccination work board" tabIndex={0}>
      {columns.map((column) => {
        const col = byColumn.get(column.key) ?? [];
        return (
          <div className="tcol" data-st={column.key} key={column.key}>
            <div className="tcolh">
              <span className="sw" style={{ background: TONE_SWATCH[column.tone] }} />
              {column.label}
              <span className="n">{col.length}</span>
            </div>
            <div className="tcards">
              {col.length ? (
                col.map((row) => (
                  <WorkCard
                    key={row.row_id}
                    row={row}
                    href={drawerHrefForRow ? drawerHrefForRow(row) : `/workflows/${encodeURIComponent(row.row_id)}`}
                  />
                ))
              ) : (
                <div className="muted small" style={{ padding: 10, textAlign: "center" }}>
                  —
                </div>
              )}
            </div>
          </div>
        );
      })}
    </div>
  );
}
