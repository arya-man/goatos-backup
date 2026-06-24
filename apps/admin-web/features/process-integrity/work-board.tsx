import Link from "next/link";
import { ArrowRight, Ban, Syringe, UserRound } from "lucide-react";
import type { ActionCenterObligation, WorkState } from "@/lib/api/server";
import {
  PROOF_META,
  SEVERITY_META,
  SOP_META,
  TONE_SWATCH,
  VERIFICATION_META,
  WORK_STATE_META,
  WORK_STATE_ORDER,
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

// One mock-shaped task card for a single Action Center obligation (ported from the mock taskCard2).
function WorkCard({ row }: { row: ActionCenterObligation }) {
  const ownerMissing = row.owner_state === "missing" || !row.owner?.operator_name;
  const title = row.drive_name ?? `${row.protocol_name} · ${row.dose_code}`;
  const href = `/workflows/${encodeURIComponent(row.row_id)}`;
  return (
    <div className="task" style={{ cursor: "default" }}>
      <div className="tt">
        <span
          className="fic"
          style={{ width: 24, height: 24, borderRadius: 7, background: "var(--brand-soft)", color: "var(--brand-d)" }}
        >
          <Syringe className="ic" style={{ width: 14 }} aria-hidden="true" />
        </span>
        <span className="ec">{row.shed_name}</span>
        <span className={`sla ${slaClass(row.work_state)}`} style={{ marginLeft: "auto" }}>
          due {fmtDate(row.due_at)}
        </span>
      </div>
      <h4 style={{ fontSize: 13 }}>{title}</h4>
      <div className="row">
        <Tag tone="mut">{row.park_name}</Tag>
        <Tag tone="info">{row.animal_stage}</Tag>
        <Tag tone={SEVERITY_META[row.severity].tone}>{SEVERITY_META[row.severity].label}</Tag>
        {row.expected_count > 0 ? (
          <span className="muted small">
            {row.completed_count}/{row.expected_count} done
          </span>
        ) : null}
      </div>
      {row.blocker_reason ? (
        <div className="row" style={{ color: "var(--danger)", marginTop: 6 }} title={row.blocker_reason}>
          <Ban className="ic" style={{ width: 13, flexShrink: 0 }} aria-hidden="true" />
          <span style={{ whiteSpace: "nowrap", overflow: "hidden", textOverflow: "ellipsis" }}>
            {row.blocker_reason.split(" — ")[0]}
          </span>
        </div>
      ) : null}
      <div className="row" style={{ marginTop: 6 }}>
        <Tag tone={SOP_META[row.sop_task_state].tone}>{SOP_META[row.sop_task_state].label}</Tag>
        <Tag tone={PROOF_META[row.proof_state].tone}>{PROOF_META[row.proof_state].label}</Tag>
        <Tag tone={VERIFICATION_META[row.verification_state].tone}>{VERIFICATION_META[row.verification_state].label}</Tag>
      </div>
      <div className="who">
        <span className="av xs">{initials(row.owner?.operator_name)}</span>
        {ownerMissing ? <span style={{ color: "var(--danger)" }}>operator: unassigned</span> : row.owner?.operator_name}
      </div>
      <div style={{ marginTop: 9, display: "flex", gap: 6, flexWrap: "wrap" }}>
        <Link href={href} className="btn sm" style={{ display: "inline-flex", alignItems: "center", gap: 4 }}>
          {row.next_action}
          <ArrowRight className="ic" style={{ width: 13, flexShrink: 0 }} aria-hidden="true" />
        </Link>
        {row.goat_id ? (
          <Link href={`/goats/${encodeURIComponent(row.goat_id)}`} className="btn sm">
            Passport
          </Link>
        ) : null}
      </div>
      {!ownerMissing && row.owner?.park_head_name ? (
        <div className="muted small" style={{ marginTop: 6, display: "flex", alignItems: "center", gap: 6 }}>
          <UserRound className="ic" style={{ width: 12, opacity: 0.7 }} aria-hidden="true" />
          {row.owner.park_head_name}
        </div>
      ) : null}
    </div>
  );
}

// Mock-shaped status board (ported from the mock taskboard): one .tcol per work state, header swatch +
// label + count, cards inside .tcards, and a "—" placeholder for empty columns. The mock always renders
// the full status set so the board shell reads as a board even when sparse; pass showAllColumns to keep
// every column, or omit it to collapse to columns that have rows. Source rows are the real
// ActionCenterResponse items — server-computed work state, not inferred.
export function WorkBoard({
  rows,
  showAllColumns = false,
}: {
  rows: ActionCenterObligation[];
  showAllColumns?: boolean;
}) {
  const byState = new Map<WorkState, ActionCenterObligation[]>();
  for (const r of rows) {
    const list = byState.get(r.work_state) ?? [];
    list.push(r);
    byState.set(r.work_state, list);
  }
  const columns = showAllColumns ? WORK_STATE_ORDER : WORK_STATE_ORDER.filter((s) => (byState.get(s)?.length ?? 0) > 0);

  return (
    <div className="taskboard" role="group" aria-label="Vaccination work board" tabIndex={0}>
      {columns.map((state) => {
        const col = byState.get(state) ?? [];
        const meta = WORK_STATE_META[state];
        return (
          <div className="tcol" data-st={state} key={state}>
            <div className="tcolh">
              <span className="sw" style={{ background: TONE_SWATCH[meta.tone] }} />
              {meta.label}
              <span className="n">{col.length}</span>
            </div>
            <div className="tcards">
              {col.length ? (
                col.map((row) => <WorkCard key={row.row_id} row={row} />)
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
