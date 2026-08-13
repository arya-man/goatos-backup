import { Layers } from "lucide-react";
import Link from "@/components/no-prefetch-link";
import { ClipText } from "@/components/ui-primitives";
import {
  copy,
  optionGroup,
  optionLabel,
  optionTitle,
  tableLabels,
  type AdminUiPageContract,
} from "@/lib/admin-ui-contract";
import type { LiveTrackerShedRow } from "@/lib/api/vaccination-live-tracker";
import { fmtClock, pct, progressTone } from "./format";
import { LiveStateTag } from "./live-state-tag";

// Sheds — proof progress. Grain is shed × partition × vaccine × operator, which is what the mock's
// rows actually are ("Gandhi 2 / Goat Pox / Kumar Sharath").
//
// The legend renders the FULL state vocabulary from the contract, including the fifth state the
// mock's own legend omitted while its rows displayed it.
export function LiveTrackerSheds({
  rows,
  total,
  truncated,
  hasFilter,
  resetHref,
  shedHref,
  pageContract,
}: {
  rows: LiveTrackerShedRow[];
  total: number;
  truncated: boolean;
  hasFilter: boolean;
  resetHref: string;
  shedHref: (row: LiveTrackerShedRow) => string;
  pageContract: AdminUiPageContract;
}) {
  const cols = tableLabels(pageContract, "live-sheds");
  const legend = optionGroup(pageContract, "live_shed_state");

  return (
    <section id="lt-sheds" className="card lt-card" style={{ scrollMarginTop: 80 }}>
      <div className="hd">
        <Layers className="ic" style={{ color: "var(--teal)" }} aria-hidden="true" />
        <h3>{copy(pageContract, "section.sheds.title")}</h3>
        <div className="sp" style={{ flex: 1 }} />
        <div className="lt-legend" aria-label={copy(pageContract, "legend.label")}>
          {legend.map((option) => (
            <span key={option.key}>
              <i className={`lt-swatch t-${option.tone || "mut"}`} />
              {option.label}
            </span>
          ))}
        </div>
      </div>

      {rows.length === 0 ? (
        <div className="bd lt-empty">
          <div style={{ minWidth: 0, flex: 1 }}>
            <b style={{ fontSize: 14 }}>
              {hasFilter ? copy(pageContract, "section.sheds.filtered_title") : copy(pageContract, "section.sheds.empty_title")}
            </b>
            <span className="muted small" style={{ display: "block", marginTop: 2, lineHeight: 1.5 }}>
              {hasFilter ? copy(pageContract, "section.sheds.filtered_body") : copy(pageContract, "section.sheds.empty_body")}
            </span>
          </div>
          {hasFilter ? (
            <Link href={resetHref} replace scroll={false} className="btn sm">
              {copy(pageContract, "action.reset_filters")}
            </Link>
          ) : null}
        </div>
      ) : (
        <div className="bd lt-tablewrap" tabIndex={0} role="group" aria-label={copy(pageContract, "section.sheds.title")}>
          <table className="lt-shed-table">
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
                // Closure is what Remaining and the status pill are derived from, so it is what the
                // bar shows. Proof arrival stays in its own column beside it.
                const tone = progressTone(row.state);
                const percent = pct(row.closed_administrations, row.scheduled_administrations);
                const href = shedHref(row);
                return (
                  <tr key={`${row.shed_id}|${row.vaccine_code}|${row.operator_id}`} className="lt-shed-row">
                    <td className="lt-shedlbl">
                      <Link
                        href={href}
                        className="celllink"
                        scroll={false}
                        aria-label={`${copy(pageContract, "section.sheds.title")} — ${row.shed_label}`}
                      >
                        <ClipText title={row.shed_label}>{row.shed_label}</ClipText>
                      </Link>
                    </td>
                    <td>{row.vaccine_label || copy(pageContract, "label.placeholder")}</td>
                    <td>
                      <ClipText title={row.operator_name}>
                        {row.operator_name || copy(pageContract, "label.placeholder")}
                      </ClipText>
                    </td>
                    <td className="num">{row.scheduled_administrations}</td>
                    <td className="num">{row.proof_videos_received}</td>
                    <td className="num">{row.closed_administrations}</td>
                    <td className="num">{row.remaining}</td>
                    <td>
                      <div className="lt-pcell">
                        <div className="lt-pbar">
                          <i className={tone} style={{ width: `${percent}%` }} />
                        </div>
                        <span className="pct">{percent}%</span>
                      </div>
                    </td>
                    <td className="muted">{fmtClock(row.last_proof_at) || copy(pageContract, "label.placeholder")}</td>
                    <td>
                      <LiveStateTag
                        pageContract={pageContract}
                        group="live_shed_state"
                        stateKey={row.state}
                        title={optionTitle(pageContract, "live_shed_state", row.state)}
                      >
                        {/* The count only leads the label when there is more than one. "1 extra
                            attempts" is the kind of small wrongness that makes a reader distrust
                            every other number on the board. */}
                        {row.state === "review" && row.extra_attempt_count > 1
                          ? `${row.extra_attempt_count} ${optionLabel(pageContract, "live_shed_state", "review")}`
                          : optionLabel(pageContract, "live_shed_state", row.state)}
                      </LiveStateTag>
                    </td>
                  </tr>
                );
              })}
            </tbody>
          </table>
          {/* The shed board is capped server-side. The tiles above are folded from the untruncated
              rollup, so past the cap they legitimately exceed this table's Scheduled column — and a
              reader can only reconcile that if the page says the table is partial. */}
          {truncated ? (
            <div className="note lt-truncnote" role="status">
              <b>
                {rows.length}/{total}
              </b>{" "}
              {copy(pageContract, "section.sheds.truncated_note")}
            </div>
          ) : null}
        </div>
      )}
    </section>
  );
}
