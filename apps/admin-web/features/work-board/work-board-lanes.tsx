import { ArrowRight } from "lucide-react";
import { LocalOverlayLink } from "@/components/local-overlay-link";
import { ClipText, Tag } from "@/components/ui-primitives";
import { copy, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import type { WorkBoardRow, WorkBoardSummary } from "@/lib/api/work-board-server";
import { TONE_SWATCH } from "@/features/process-integrity";
import { clockClass, findOption, initials, lanes, moduleOptions, needsAttention, stateOptions, toneOf } from "./work-board-model";

// One card per board row, in the mock's task-card shape. Everything visible is backend-owned:
// the title, the clock label, the pen display, the module and state labels, the counts.
function WorkCard({ pageContract, row, href }: { pageContract: AdminUiPageContract; row: WorkBoardRow; href: string }) {
  const moduleOpt = findOption(moduleOptions(pageContract), row.module);
  const stateOpt = findOption(stateOptions(pageContract), row.work_state);
  const ownerMissing = row.owner_state === "missing";
  const ownerLabel = row.owner?.name || (row.owner_state === "pool" ? copy(pageContract, "owner.pool") : copy(pageContract, "owner.missing"));
  const hot = needsAttention(row);
  const total = row.counts.done + row.counts.pending;
  return (
    <LocalOverlayLink
      href={href}
      scroll={false}
      className={`task task-ac${hot ? " task-hot" : ""}`}
      data-filter-row
      aria-label={row.title}
      title={`${row.title} · ${ownerLabel}`}
      style={{ color: "inherit", textDecoration: "none" }}
    >
      <div className="tt">
        <span className={`sla ${clockClass(row)}`} title={row.clock_label}>
          {row.clock_label}
        </span>
      </div>
      <h4 title={row.title}>{row.title}</h4>
      {row.subtitle ? (
        <div className="muted small ac-drive" title={row.subtitle}>
          {row.subtitle}
        </div>
      ) : null}
      <div className="row">
        <Tag tone={toneOf(moduleOpt)}>{moduleOpt?.label ?? row.module}</Tag>
        <Tag tone="info">{row.park_name || row.park_id}</Tag>
        <Tag tone={toneOf(stateOpt)}>{stateOpt?.label ?? row.work_state}</Tag>
      </div>
      <div className="row" style={{ marginTop: 6 }}>
        {total > 0 ? (
          <span className="muted small ac-progress">
            {row.counts.done}/{total} {copy(pageContract, "card.done")}
          </span>
        ) : null}
        {row.counts.needs_attention > 0 ? (
          <span className="small ac-progress" style={{ color: "var(--amber)" }}>
            {row.counts.needs_attention} {copy(pageContract, "card.attention")}
          </span>
        ) : null}
      </div>
      <div className="who">
        <span className="av xs">{initials(row.owner?.name)}</span>
        <ClipText title={ownerLabel} style={ownerMissing ? { color: "var(--danger)" } : undefined}>
          {ownerLabel}
        </ClipText>
        <ArrowRight className="ic" style={{ width: 13, marginLeft: "auto", flexShrink: 0 }} aria-hidden="true" />
      </div>
    </LocalOverlayLink>
  );
}

// The four columns. Lane membership is the SERVER's `lane`; the header count is the whole-filter
// summary, never the length of the fetched page.
export function WorkBoardLanes({
  pageContract,
  rows,
  summary,
  drawerHrefForRow,
}: {
  pageContract: AdminUiPageContract;
  rows: WorkBoardRow[];
  summary: WorkBoardSummary | null;
  drawerHrefForRow: (row: WorkBoardRow) => string;
}) {
  const columns = lanes(pageContract);
  const byLane = new Map<string, WorkBoardRow[]>();
  for (const row of rows) {
    const list = byLane.get(row.lane) ?? [];
    list.push(row);
    byLane.set(row.lane, list);
  }
  return (
    <div className="taskboard" role="group" aria-label={copy(pageContract, "section.board.aria")} tabIndex={0}>
      {columns.map((column) => {
        const list = byLane.get(column.key) ?? [];
        const count = summary ? summary.by_lane[column.key] ?? 0 : list.length;
        return (
          <div className="tcol" data-st={column.key} key={column.key} title={column.title}>
            <div className="tcolh">
              <span className="sw" style={{ background: TONE_SWATCH[column.tone] }} />
              {column.label}
              <span className="n">{count}</span>
            </div>
            <div className="tcards">
              {list.length ? (
                list.map((row) => <WorkCard key={row.row_key} pageContract={pageContract} row={row} href={drawerHrefForRow(row)} />)
              ) : (
                <div className="muted small" style={{ padding: 10, textAlign: "center" }}>
                  {copy(pageContract, "lane.empty")}
                </div>
              )}
            </div>
          </div>
        );
      })}
    </div>
  );
}
