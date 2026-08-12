import { copy, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import type { LiveTrackerKPIs } from "@/lib/api/vaccination-live-tracker";

// The six headline tiles. Five are ADMINISTRATION grain; "Combo animals" is explicitly ANIMAL grain
// and says so in its own detail line, because mixing the two inside one number is the single easiest
// way to make this page lie.
//
// Every figure comes from the server's own aggregate over the same membership set the tables below
// render, so a tile can never disagree with the table under it.
export function LiveTrackerKpis({
  kpis,
  pageContract,
}: {
  kpis: LiveTrackerKPIs;
  pageContract: AdminUiPageContract;
}) {
  const parkSplit = kpis.scheduled_by_park
    .map((park) => `${park.count} ${park.park_name}`)
    .join(" + ");
  const crossFilterReason = copy(pageContract, "kpi.cross_filter_disabled");

  const tiles: Array<{ key: string; modifier: string; label: string; value: number; detail: string; tick?: boolean }> = [
    {
      key: "scheduled",
      modifier: "",
      label: copy(pageContract, "kpi.scheduled.label"),
      value: kpis.scheduled_administrations,
      detail: parkSplit
        ? `${copy(pageContract, "kpi.scheduled.detail")} · ${parkSplit}`
        : copy(pageContract, "kpi.scheduled.detail"),
    },
    {
      key: "proofs",
      modifier: "k-ok",
      label: copy(pageContract, "kpi.proofs.label"),
      value: kpis.proof_videos_received,
      detail: copy(pageContract, "kpi.proofs.detail"),
      tick: true,
    },
    {
      key: "scans",
      modifier: "k-info",
      label: copy(pageContract, "kpi.scans.label"),
      value: kpis.scan_captures,
      detail: copy(pageContract, "kpi.scans.detail"),
      tick: true,
    },
    {
      key: "remaining",
      modifier: "k-warn",
      label: copy(pageContract, "kpi.remaining.label"),
      value: kpis.remaining,
      detail: copy(pageContract, "kpi.remaining.detail"),
    },
    {
      key: "combo",
      modifier: "k-purple",
      label: copy(pageContract, "kpi.combo.label"),
      value: kpis.combo_animals,
      detail: copy(pageContract, "kpi.combo.detail"),
    },
    {
      key: "attention",
      modifier: "k-dng",
      label: copy(pageContract, "kpi.attention.label"),
      value: kpis.attention_count,
      detail: copy(pageContract, "kpi.attention.detail"),
    },
  ];

  return (
    <div className="lt-kpis">
      {tiles.map((tile) => (
        // The mock gives every tile a pointer cursor implying a cross-filter that it never wired.
        // Rendering it as an inert div with a visible reason is the honest form: the control stays
        // where the mock put it, and says why it does nothing.
        <div
          key={tile.key}
          className={`kpi lt-kpi${tile.modifier ? ` ${tile.modifier}` : ""}`}
          aria-disabled="true"
          title={crossFilterReason}
        >
          <div className="lab">{tile.label}</div>
          <div className="val">{tile.value}</div>
          <div className="dl">
            {tile.detail}
            {tile.tick ? <span className="lt-tick" data-live-tick={tile.key}>{copy(pageContract, "kpi.live_tick")}</span> : null}
          </div>
        </div>
      ))}
    </div>
  );
}
