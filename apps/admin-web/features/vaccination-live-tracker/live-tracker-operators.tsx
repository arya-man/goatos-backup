import { User } from "lucide-react";
import { Tag, ClipText, type Tone } from "@/components/ui-primitives";
import Link from "@/components/no-prefetch-link";
import {
  copy,
  optionLabel,
  optionTone,
  optionTitle,
  tableLabels,
  type AdminUiPageContract,
} from "@/lib/admin-ui-contract";
import type { LiveTrackerOperatorRow } from "@/lib/api/vaccination-live-tracker";
import { fmtClock, initials, pct, progressTone } from "./format";

// Operators — live. One row per operator on the drive day, at ADMINISTRATION grain: the sum of the
// Scheduled column equals the Scheduled tile for the same filter set.
export function LiveTrackerOperators({
  rows,
  parkCount,
  hasFilter,
  resetHref,
  pageContract,
}: {
  rows: LiveTrackerOperatorRow[];
  parkCount: number;
  hasFilter: boolean;
  resetHref: string;
  pageContract: AdminUiPageContract;
}) {
  const cols = tableLabels(pageContract, "live-operators");
  const identityReason = copy(pageContract, "section.operators.unavailable");

  return (
    <section id="lt-operators" className="card lt-card" style={{ scrollMarginTop: 80 }}>
      <div className="hd">
        <User className="ic" style={{ color: "var(--info)" }} aria-hidden="true" />
        <h3>{copy(pageContract, "section.operators.title")}</h3>
        <span className="tag t-mut">
          {rows.length} {copy(pageContract, "section.operators.count_suffix")} · {parkCount}{" "}
          {copy(pageContract, "section.operators.park_suffix")}
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
                  <th key={label} className={index >= 3 && index <= 6 ? "num" : undefined} style={index === 7 ? { minWidth: 150 } : undefined}>
                    {label}
                  </th>
                ))}
              </tr>
            </thead>
            <tbody>
              {rows.map((row) => {
                const done = row.proof_videos;
                const tone = progressTone(done, row.scheduled_administrations);
                const stateKey = row.state;
                const subLine = row.current_vaccine_label
                  ? row.current_vaccine_label
                  : row.last_activity_at
                    ? fmtClock(row.last_activity_at)
                    : optionLabel(pageContract, "live_operator_state", "not_started");
                return (
                  <tr key={row.operator_id}>
                    <td>
                      <span className="lt-opname">
                        <span className="lt-avx" aria-hidden="true">{initials(row.operator_name)}</span>
                        <ClipText title={row.operator_name}>{row.operator_name}</ClipText>
                      </span>
                      {/* Operator identity is only partially seeded in some environments: the code
                          is a raw auth subject rather than a workforce display code. The cell stays
                          where the mock put it and states the reason instead of showing the token. */}
                      {!row.identity_resolved ? (
                        <span className="muted small lt-code" aria-disabled="true" title={identityReason}>
                          {copy(pageContract, "label.placeholder")}
                        </span>
                      ) : (
                        <span className="muted small lt-code">{row.operator_display_code}</span>
                      )}
                    </td>
                    <td>{row.park_code || row.park_name || copy(pageContract, "label.placeholder")}</td>
                    <td className="lt-shedlbl">
                      {row.current_shed_label || copy(pageContract, "label.placeholder")}
                      <small>{subLine}</small>
                    </td>
                    <td className="num">{row.scheduled_administrations}</td>
                    <td className="num">{row.proof_videos}</td>
                    <td className="num">{row.scan_captures}</td>
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
                      <Tag
                        tone={optionTone(pageContract, "live_operator_state", stateKey) as Tone}
                        title={optionTitle(pageContract, "live_operator_state", stateKey)}
                      >
                        {optionLabel(pageContract, "live_operator_state", stateKey)}
                      </Tag>
                    </td>
                  </tr>
                );
              })}
            </tbody>
          </table>
        </div>
      )}
    </section>
  );
}
