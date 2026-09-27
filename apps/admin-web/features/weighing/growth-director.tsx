import Alert from "@mui/material/Alert";
import AlertTitle from "@mui/material/AlertTitle";
import Box from "@mui/material/Box";
import Card from "@mui/material/Card";
import CardHeader from "@mui/material/CardHeader";
import Grid from "@mui/material/Grid";
import Stack from "@mui/material/Stack";
import Typography from "@mui/material/Typography";

import { EmptyState } from "@/components/app/empty-state";
import { Label } from "@/components/minimal/label";
import { KpiGrid } from "@/components/app/kpi-grid";
import { KpiWidget, kpiColor } from "@/components/app/kpi-widget";
import type { KitTone } from "@/lib/tone";
import { EcommerceSalesOverview } from "@/components/minimal/sections/overview/e-commerce/ecommerce-sales-overview";
import { copy, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import type { ApiResult, GrowthDirectorWeightsResponse } from "@/lib/api/server";
import { fmtQty } from "@/lib/format";

// Growth Director — the analytics block UNDER the live Weights dashboard.
//
// Server component, no client JS. Every visible string resolves through the
// weighing-weights page contract: the `growth_director.*` vocabulary is
// BACKEND-OWNED (adminui service copy map) and this component renders that
// vocabulary 1:1 — COPY_FALLBACKS carries the same keys only as the offline
// fallback. Every widget states its own denominator, because the whole block
// stands on partial data (only kids weighed twice have a gain, only
// tag-matched kids have a breed/sex) and hiding that would let a thin sample
// read as a herd-wide fact.
//
// The block renders BACKEND aggregates verbatim — no client-side math beyond
// choosing what to show. Ratios like "kg of feed per kg gained" arrive
// computed; recomputing here from a row slice is the capped-rollup
// anti-pattern the page header warns about. There is deliberately no
// roster-count denominator anywhere: weighing is free-flow and has no roster
// (that table was dropped), so every denominator below counts scans and kids
// actually seen.

const nf = (value: number) => value.toLocaleString("en-IN", { maximumFractionDigits: 1 });

function gd(pageContract: AdminUiPageContract, key: string): string {
  return copy(pageContract, `growth_director.${key}`);
}

export function GrowthDirectorSection({
  result,
  pageContract,
}: {
  result: ApiResult<GrowthDirectorWeightsResponse>;
  pageContract: AdminUiPageContract;
}) {
  // A Growth Director failure must never take the Weights page down: the live
  // dashboard above is independent and stays useful without this block.
  if (!result.ok) {
    return (
      <Alert severity="error" variant="outlined" aria-label={gd(pageContract, "section.aria")}>
        <AlertTitle>{gd(pageContract, "error.title")}</AlertTitle>
        {gd(pageContract, "error.body")}
      </Alert>
    );
  }

  const { road_to_sale: road, fair_fight: fairFight } = result.data;
  // `feed_problems`, `trust`, `slow_growth` and `feed_vs_growth` are still served by the
  // backend, but the Feed sheet problems table, the trust-panel KPI row, the Slow-growth
  // watchlist and the Feed given vs growth table were removed from this page (maintainer
  // decisions 2026-08-15 / 2026-08-17); the contract keeps them so they can be restored.
  const noData = copy(pageContract, "empty.no_data.title");
  const bandTotal = road.bands.reduce((sum, band) => sum + band.animal_count, 0);
  const kidsNoun = gd(pageContract, "fair_fight.pair_noun");

  return (
    <Grid container spacing={3} component="section" aria-label={gd(pageContract, "section.aria")}>
      <Grid size={12}>
        <Typography variant="h5" component="h2">
          {gd(pageContract, "section.title")}
        </Typography>
      </Grid>

      {/* ---------------- Road to sale weight ----------------
          Total animals · lump-sum · pair denominator · moved up · held · slipped: one template
          InvoiceAnalytic strip, so the three movement figures visibly sum to the pairs cell. */}
      <Grid size={12}>
        <Card aria-label={gd(pageContract, "road.title")}>
          <CardHeader title={gd(pageContract, "road.title")} subheader={gd(pageContract, "road.caption")} sx={{ mb: 1 }} />
          <Box sx={{ px: 3, pb: 3 }}>
            <KpiGrid>
              {([
              { key: "total", label: gd(pageContract, "road.identities.sub"), value: road.total_animals, tone: "primary" as const },
              { key: "lump", label: gd(pageContract, "road.lump.sub"), value: road.lump_sum_animals, tone: "info" as const },
              { key: "pairs", label: gd(pageContract, "road.pairs.sub"), value: road.movement.pair_animals, tone: "neutral" as const },
              { key: "up", label: gd(pageContract, "road.moved_up"), value: road.movement.moved_up, tone: "success" as const },
              { key: "held", label: gd(pageContract, "road.held"), value: road.movement.held, tone: "warning" as const },
              { key: "down", label: gd(pageContract, "road.moved_down"), value: road.movement.moved_down, tone: road.movement.moved_down > 0 ? ("error" as const) : ("neutral" as const) },
              ] as { key: string; label: string; value: number; tone: KitTone }[]).map((cell) => (
                <KpiWidget key={cell.key} title={cell.label} total={cell.value} color={kpiColor(cell.tone)} sx={{ height: 1, boxShadow: "none", border: 1, borderColor: "divider" }} />
              ))}
            </KpiGrid>
          </Box>
        </Card>
      </Grid>
      {/* The six bands ARE the distribution: all six rows, each its share of the kids banded. */}
      <Grid size={{ xs: 12, md: 6 }}>
        <EcommerceSalesOverview
          title={gd(pageContract, "road.title")}
          subheader={gd(pageContract, "period.note")}
          data={road.bands.map((band) => ({
            key: band.band,
            label: band.band,
            value: bandTotal > 0 ? (band.animal_count / bandTotal) * 100 : 0,
            display: `${nf(band.animal_count)} ${kidsNoun}`,
          }))}
        >
          {bandTotal === 0 ? <EmptyState title={copy(pageContract, "empty.no_data.body")} /> : null}
          <Typography variant="body2" sx={{ color: "text.secondary" }}>
            {gd(pageContract, "road.note.pairs")} {gd(pageContract, "road.note.lump")} {gd(pageContract, "road.note.unmatched")}
          </Typography>
        </EcommerceSalesOverview>
      </Grid>

      {/* ---------------- Fair fight ----------------
          Each cohort is STANDINGS on the template sales-overview card: the backend returns a
          cohort's sheds ordered by median gain DESC, so the leader/behind Labels and the spread
          come from that ranked array by VALUE, while the rows are LISTED alphabetically
          (maintainer decision 2026-08-24) so a reader can find the pen they came for. Each bar
          is drawn against the cohort's OWN best ("share of the leader"). */}
      {/* Right rail: the fair-fight intro card, then one standings card per cohort under it. */}
      <Grid size={{ xs: 12, md: 6 }}>
        <Stack spacing={3}>
        <Card aria-label={gd(pageContract, "fair_fight.title")}>
          <CardHeader title={gd(pageContract, "fair_fight.title")} subheader={gd(pageContract, "fair_fight.caption")} />
          <Typography variant="body2" sx={{ p: 3, color: "text.secondary" }}>
            {gd(pageContract, "fair_fight.note")}
          </Typography>
          {fairFight.cohorts.length === 0 ? <EmptyState title={noData} description={gd(pageContract, "fair_fight.empty")} /> : null}
        </Card>
      {fairFight.cohorts.map((cohort) => {
        const ranked = cohort.sheds;
        const best = ranked[0];
        const last = ranked[ranked.length - 1];
        const rankByKey = new Map(ranked.map((shed, index) => [shed.operational_key, index]));
        const sheds = [...ranked].sort((a, b) =>
          a.shed_display_name.localeCompare(b.shed_display_name, undefined, { numeric: true }),
        );
        const spread = ranked.length > 1 ? best.median_adg_g_per_day - last.median_adg_g_per_day : null;
        const kids = ranked.reduce((sum, shed) => sum + shed.pair_identities, 0);
        return (
            <EcommerceSalesOverview
              key={`${cohort.breed}-${cohort.sex}`}
              aria-label={`${gd(pageContract, "fair_fight.title")} — ${cohort.breed} ${cohort.sex}`}
              title={`${cohort.breed} · ${cohort.sex}`}
              subheader={`${nf(sheds.length)} ${gd(pageContract, "fair_fight.shed_noun")} · ${nf(kids)} ${kidsNoun}`}
              data={sheds.map((shed) => {
                const standing = rankByKey.get(shed.operational_key) ?? 0;
                const isLeader = standing === 0 && ranked.length > 1;
                const isLast = standing === ranked.length - 1 && ranked.length > 1;
                return {
                  key: shed.operational_key,
                  label: shed.shed_display_name,
                  // A non-positive leader leaves every track empty: there is no gain to share.
                  value: best.median_adg_g_per_day > 0 ? Math.max(0, (shed.median_adg_g_per_day / best.median_adg_g_per_day) * 100) : 0,
                  display: `${fmtQty(shed.median_adg_g_per_day)} g`,
                  color: shed.median_adg_g_per_day < 0 ? ("error" as const) : isLeader ? ("primary" as const) : ("info" as const),
                  caption: (
                    <>
                      {nf(shed.pair_identities)} {kidsNoun}{" "}
                      {isLeader ? (
                        <Label color="success" variant="soft">{gd(pageContract, "fair_fight.leader")}</Label>
                      ) : isLast ? (
                        <Label variant="soft">{gd(pageContract, "fair_fight.behind")}</Label>
                      ) : null}
                    </>
                  ),
                };
              })}
            >
              {spread === null ? null : (
                <Typography variant="body2" sx={{ color: "text.secondary" }}>
                  {gd(pageContract, "fair_fight.spread")}{" "}
                  <Typography component="span" variant="subtitle2" sx={{ color: "text.primary" }}>
                    {nf(spread)} g
                  </Typography>
                </Typography>
              )}
            </EcommerceSalesOverview>
        );
      })}
        </Stack>
      </Grid>
    </Grid>
  );
}
