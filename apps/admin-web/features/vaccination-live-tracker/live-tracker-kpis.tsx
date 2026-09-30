import Alert from "@mui/material/Alert";
import Box from "@mui/material/Box";
import type { KitTone } from "@/lib/tone";
import { LT_KPI_MIN } from "./live-tracker-layout";
import { KpiGrid } from "@/components/app/kpi-grid";
import { KpiWidget, kpiColor } from "@/components/app/kpi-widget";
import { copy, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import type { LiveTrackerKPIs } from "@/lib/api/vaccination-live-tracker";
import { LiveTick } from "./live-tick";

// The six headline tiles. Five are ADMINISTRATION grain; "Combo animals" is explicitly ANIMAL grain
// and says so in its own detail line, because mixing the two inside one number is the single easiest
// way to make this page lie.
//
// Every figure comes from the server's own aggregate over the same membership set the tables below
// render, so a tile can never disagree with the table under it.
export function LiveTrackerKpis({
  kpis,
  truncated,
  pageContract,
}: {
  kpis: LiveTrackerKPIs;
  truncated: boolean;
  pageContract: AdminUiPageContract;
}) {
  // The mock's detail line is "administrations · 153 CBE + 145 CPT" — park CODES, because the full
  // names ("153 Coimbatore + 145 Channapatna") overflow a 150px-minimum tile. The code is now
  // carried on the park count; the name is the fallback when no location_code is seeded.
  const parkSplit = kpis.scheduled_by_park
    .map((park) => `${park.count} ${park.park_code || park.park_name}`)
    .join(" + ");
  const crossFilterReason = copy(pageContract, "kpi.cross_filter_disabled");

  const tiles: Array<{ key: string; tone: KitTone; label: string; value: number; detail: string; tick?: boolean }> = [
    {
      key: "scheduled",
      tone: "primary",
      label: copy(pageContract, "kpi.scheduled.label"),
      value: kpis.scheduled_administrations,
      detail: parkSplit
        ? `${copy(pageContract, "kpi.scheduled.detail")} · ${parkSplit}`
        : copy(pageContract, "kpi.scheduled.detail"),
    },
    {
      key: "proofs",
      tone: "success",
      label: copy(pageContract, "kpi.proofs.label"),
      value: kpis.proof_videos_received,
      detail: copy(pageContract, "kpi.proofs.detail"),
      tick: true,
    },
    {
      key: "scans",
      tone: "info",
      label: copy(pageContract, "kpi.scans.label"),
      value: kpis.scan_captures,
      detail: copy(pageContract, "kpi.scans.detail"),
      tick: true,
    },
    {
      // Remaining counts administrations whose OBLIGATION is not closed, not administrations without
      // a proof. Those are different facts and the gap between them is the whole operational point:
      // in stg on 2026-08-12 all 298 videos had landed while 9 of 298 obligations were completed, so
      // a Remaining derived from proof arrival read 0 on a drive that was still open. The proofed-
      // but-open figure rides on the same tile so the reader can see both without a seventh tile.
      key: "remaining",
      tone: "warning",
      label: copy(pageContract, "kpi.remaining.label"),
      value: kpis.remaining,
      detail:
        kpis.awaiting_close > 0
          ? `${copy(pageContract, "kpi.remaining.detail")} · ${kpis.awaiting_close} ${copy(pageContract, "kpi.remaining.awaiting_prefix")}`
          : copy(pageContract, "kpi.remaining.detail"),
    },
    {
      key: "combo",
      tone: "violet",
      label: copy(pageContract, "kpi.combo.label"),
      value: kpis.combo_animals,
      detail: copy(pageContract, "kpi.combo.detail"),
    },
    {
      key: "attention",
      tone: "error",
      label: copy(pageContract, "kpi.attention.label"),
      value: kpis.attention_count,
      detail: copy(pageContract, "kpi.attention.detail"),
    },
  ];

  return (
    <>
      {/* The rollup these tiles are folded from is itself capped. Past that cap the headline number
          under-reports the drive day by an unbounded amount, which is a wrong number rather than an
          error — so it is stated out loud instead of shipped silently. */}
      {truncated ? (
        <Alert severity="warning" role="status" data-truncnote="">
          {copy(pageContract, "kpi.truncated_note")}
        </Alert>
      ) : null}
      {/* A deck of zeros is a wall, not a reading: the tiles render only once the day has counts. */}
      {tiles.some((tile) => Number(tile.value) > 0) ? (
      <KpiGrid min={LT_KPI_MIN}>
        {tiles.map((tile) => (
          // The mock gives every tile a pointer cursor implying a cross-filter that it never wired.
          // Rendering it inert with a visible reason is the honest form: the control stays where the
          // mock put it, and says why it does nothing.
          <Box key={tile.key} title={crossFilterReason} aria-disabled="true" sx={{ minWidth: 0 }}>
            <KpiWidget title={tile.label} total={tile.value} color={kpiColor(tile.tone)} caption={tile.detail} sx={{ height: 1 }} />
            {tile.tick ? <LiveTick value={tile.value} label={copy(pageContract, "kpi.live_tick")} /> : null}
          </Box>
        ))}
      </KpiGrid>
      ) : null}
    </>
  );
}
