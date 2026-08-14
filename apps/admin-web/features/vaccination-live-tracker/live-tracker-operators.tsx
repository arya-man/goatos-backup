import { User } from "lucide-react";
import { ClipText } from "@/components/ui-primitives";
import Link from "@/components/no-prefetch-link";
import {
  copy,
  optionLabel,
  optionTitle,
  tableLabels,
  type AdminUiPageContract,
} from "@/lib/admin-ui-contract";
import type { LiveTrackerOperatorRow } from "@/lib/api/vaccination-live-tracker";
import { fmtClock, initials, pct, progressTone } from "./format";
import { LiveStateTag } from "./live-state-tag";

// Operators — live. One row per operator on the drive day, at ADMINISTRATION grain.
//
// The sum of the Scheduled column equals the Scheduled tile MINUS unassignedAdmins: work in a
// shed/partition with no drive assignment for the day has no operator to be attributed to, so it is
// counted in the tiles and listed under Sheds but appears in no row here. That residual is rendered
// as its own note rather than left as an unexplained gap between a tile and the table under it — on
// a partially-planned stg day it is 95 of 199.
export function LiveTrackerOperators({
  rows,
  parkCount,
  total,
  truncated,
  unassignedAdmins,
  hasFilter,
  resetHref,
  pageContract,
}: {
  rows: LiveTrackerOperatorRow[];
  parkCount: number;
  total: number;
  truncated: boolean;
  unassignedAdmins: number;
  hasFilter: boolean;
  resetHref: string;
  pageContract: AdminUiPageContract;
}) {
  const cols = tableLabels(pageContract, "live-operators");

  return (
    <section id="lt-operators" className="card lt-card" style={{ scrollMarginTop: 80 }}>
      <div className="hd">
        <User className="ic" style={{ color: "var(--info)" }} aria-hidden="true" />
        <h3>{copy(pageContract, "section.operators.title")}</h3>
        <span className="tag t-mut">
          {rows.length}{" "}
          {rows.length === 1
            ? copy(pageContract, "section.operators.count_suffix_one")
            : copy(pageContract, "section.operators.count_suffix")}{" "}
          · {parkCount}{" "}
          {parkCount === 1
            ? copy(pageContract, "section.operators.park_suffix_one")
            : copy(pageContract, "section.operators.park_suffix")}
        </span>
        <div className="sp" style={{ flex: 1 }} />
        <span className="small muted">{copy(pageContract, "section.operators.drilldown_note")}</span>
      </div>

      {rows.length === 0 ? (
        <div className="bd lt-empty">
          <div style={{ minWidth: 0, flex: 1 }}>
            <b style={{ fontSize: 14 }}>
              {hasFilter
                ? copy(pageContract, "section.operators.filtered_title")
                : copy(pageContract, "section.operators.empty_title")}
            </b>
            <span className="muted small" style={{ display: "block", marginTop: 2, lineHeight: 1.5 }}>
              {hasFilter
                ? copy(pageContract, "section.operators.filtered_body")
                : copy(pageContract, "section.operators.empty_body")}
            </span>
          </div>
          {hasFilter ? (
            <Link href={resetHref} replace scroll={false} className="btn sm">
              {copy(pageContract, "action.reset_filters")}
            </Link>
          ) : null}
        </div>
      ) : (
        <div
          className="bd lt-tablewrap"
          tabIndex={0}
          role="group"
          aria-label={copy(pageContract, "section.operators.title")}
        >
          <table className="lt-operator-table">
            <thead>
              <tr>
                {cols.map((label, index) => (
                  <th key={label} className={index >= 3 && index <= 7 ? "num" : undefined} style={index === 8 ? { minWidth: 150 } : undefined}>
                    {label}
                  </th>
                ))}
              </tr>
            </thead>
            <tbody>
              {rows.map((row) => {
                // The bar tracks CLOSURE, the same fact Remaining is derived from, so the bar, the
                // Remaining cell and the status pill all describe one thing.
                const done = row.closed_administrations;
                const tone = progressTone(row.state);
                const stateKey = row.state;
                const subLine = row.current_vaccine_label
                  ? row.current_vaccine_label
                  : row.last_activity_at
                    ? `${copy(pageContract, "section.operators.now_at_prefix")} ${fmtClock(row.last_activity_at)}`
                    : optionLabel(pageContract, "live_operator_state", "not_started");
                // The mock's idle pill carries the duration ("idle 2h+", mock line 261): "idle" with
                // no number gives a director nothing to act on. idle_minutes is already returned.
                const stateLabel =
                  stateKey === "idle" && row.idle_minutes != null
                    ? `${copy(pageContract, "section.operators.idle_prefix")} ${row.idle_minutes} ${copy(pageContract, "section.operators.idle_suffix")}`
                    : optionLabel(pageContract, "live_operator_state", stateKey);
                return (
                  <tr key={row.operator_id}>
                    <td>
                      <span className="lt-opname">
                        <span className="lt-avx" aria-hidden="true">{initials(row.operator_name)}</span>
                        <ClipText title={row.operator_name}>{row.operator_name}</ClipText>
                      </span>
                    </td>
                    <td>{row.park_code || row.park_name || copy(pageContract, "label.placeholder")}</td>
                    <td className="lt-shedlbl">
                      {row.current_shed_label || copy(pageContract, "label.placeholder")}
                      <small>{subLine}</small>
                    </td>
                    <td className="num">{row.scheduled_administrations}</td>
                    <td className="num">{row.proof_videos}</td>
                    <td className="num">{row.scan_captures}</td>
                    <td className="num">{row.closed_administrations}</td>
                    <td className="num">{row.remaining}</td>
                    <td>
                      <div className="lt-pcell">
                        <div className="lt-pbar">
                          <i className={tone} style={{ width: `${pct(done, row.scheduled_administrations)}%` }} />
                        </div>
                        <span className="pct">
                          {done}/{row.scheduled_administrations}
                        </span>
                      </div>
                    </td>
                    <td>
                      <LiveStateTag
                        pageContract={pageContract}
                        group="live_operator_state"
                        stateKey={stateKey}
                        title={optionTitle(pageContract, "live_operator_state", stateKey)}
                      >
                        {stateLabel}
                      </LiveStateTag>
                    </td>
                  </tr>
                );
              })}
            </tbody>
          </table>
          {/* Rows may never vanish silently. The KPI tiles are folded from the untruncated rollup,
              so past the cap the Scheduled tile legitimately reads higher than this table sums —
              which is unreadable unless the page says why. */}
          {truncated ? (
            <div className="note lt-truncnote" role="status">
              <b>
                {rows.length}/{total}
              </b>{" "}
              {copy(pageContract, "section.operators.truncated_note")}
            </div>
          ) : null}
          {/* The residual is stated, never left implicit. Without it the Scheduled column simply
              sums short of the tile above with nothing on screen to explain the difference. */}
          {unassignedAdmins > 0 ? (
            <div className="note lt-truncnote" role="status">
              <b>{unassignedAdmins}</b> {copy(pageContract, "section.operators.unassigned_note")}
            </div>
          ) : null}
        </div>
      )}
    </section>
  );
}
