"use client";

import Link from "@/components/no-prefetch-link";
import { LocalOverlayDrawer, type LocalOverlayDrawerItem } from "@/components/local-overlay-drawer";
import { Tag } from "@/components/ui-primitives";
import { copy, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import type { WorkBoardRow } from "@/lib/api/work-board-server";
import { fmtDate } from "@/lib/format";
import { findOption, lanes, moduleOptions, PARAM_ROW, stateOptions, toneOf } from "./work-board-model";

// The card's detail, opened as a local overlay: no navigation, no document request. Every
// string is backend-owned copy or a field the backend composed; the tiles are the row's own
// counts. Subtask paging inside the drawer waits on a per-module detail read (build plan,
// phase 3 follow-up), so this drawer shows the row and where to open it in its module.
function Detail({ pageContract, row }: { pageContract: AdminUiPageContract; row: WorkBoardRow }) {
  const moduleOpt = findOption(moduleOptions(pageContract), row.module);
  const stateOpt = findOption(stateOptions(pageContract), row.work_state);
  const laneOpt = lanes(pageContract).find((lane) => lane.key === row.lane);
  const total = row.counts.done + row.counts.pending;
  const ownerLabel = row.owner?.name || (row.owner_state === "pool" ? copy(pageContract, "owner.pool") : copy(pageContract, "owner.missing"));
  const detail = (label: string, value: React.ReactNode) => (
    <div className="kv" style={{ display: "grid", gridTemplateColumns: "120px 1fr", gap: 12, padding: "8px 0", borderBottom: "1px solid var(--line2)" }}>
      <span className="muted small">{label}</span>
      <span>{value}</span>
    </div>
  );
  return (
    <div>
      <div className="row" style={{ gap: 8, flexWrap: "wrap" }}>
        <Tag tone={toneOf(moduleOpt)}>{moduleOpt?.label ?? row.module}</Tag>
        <Tag tone={toneOf(stateOpt)} title={copy(pageContract, "drawer.status_auto.title")}>
          {laneOpt?.label ?? row.lane} · {copy(pageContract, "drawer.status_auto")}
        </Tag>
      </div>
      {row.subtitle ? <p className="muted" style={{ marginTop: 10 }}>{row.subtitle}</p> : null}
      <div style={{ display: "grid", gridTemplateColumns: "repeat(3, 1fr)", gap: 8, marginTop: 12 }}>
        <div className="kpi">
          <div className="lab">{copy(pageContract, "tile.done")}</div>
          <div className="val" style={{ color: "var(--brand)" }}>
            {row.counts.done}
            {total > 0 ? <small className="muted"> / {total}</small> : null}
          </div>
        </div>
        <div className="kpi">
          <div className="lab">{copy(pageContract, "tile.pending")}</div>
          <div className="val" style={{ color: "var(--info)" }}>{row.counts.pending}</div>
        </div>
        <div className="kpi" style={row.counts.needs_attention > 0 ? { borderColor: "var(--amber)" } : undefined}>
          <div className="lab">{copy(pageContract, "tile.attention")}</div>
          <div className="val" style={row.counts.needs_attention > 0 ? { color: "var(--amber)" } : undefined}>{row.counts.needs_attention}</div>
        </div>
      </div>
      <h3 style={{ margin: "18px 0 6px", fontSize: 14 }}>{copy(pageContract, "drawer.details")}</h3>
      {detail(copy(pageContract, "detail.status"), stateOpt?.label ?? row.work_state)}
      {detail(copy(pageContract, "detail.module"), moduleOpt?.label ?? row.module)}
      {detail(copy(pageContract, "detail.park"), row.park_name || row.park_id)}
      {detail(copy(pageContract, "detail.pen"), row.pen.operational_location_display || "—")}
      {detail(copy(pageContract, "detail.owner"), ownerLabel)}
      {detail(copy(pageContract, "detail.clock"), row.clock_label || "—")}
      {detail(copy(pageContract, "detail.business_date"), fmtDate(row.business_date))}
    </div>
  );
}

export function WorkBoardDrawer({
  pageContract,
  rows,
  initialSelectedRowKey,
  closeHref,
}: {
  pageContract: AdminUiPageContract;
  rows: WorkBoardRow[];
  initialSelectedRowKey?: string;
  closeHref: string;
}) {
  const items: LocalOverlayDrawerItem[] = rows.map((row) => ({
    id: row.row_key,
    eyebrow: copy(pageContract, "drawer.title"),
    title: row.title,
    body: <Detail pageContract={pageContract} row={row} />,
    footer: row.href ? (
      <Link href={row.href} className="btn sm p">
        {copy(pageContract, "drawer.open_module")}
      </Link>
    ) : undefined,
  }));
  return (
    <LocalOverlayDrawer
      items={items}
      selectionKey={PARAM_ROW}
      initialSelectedId={initialSelectedRowKey}
      closeHref={closeHref}
      ariaLabel={copy(pageContract, "drawer.title")}
      closeLabel={copy(pageContract, "action.close")}
    />
  );
}
