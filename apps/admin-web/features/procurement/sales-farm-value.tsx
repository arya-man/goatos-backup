import { UrlSuspense } from "@/components/app/url-suspense";
import { PanelSkeleton } from "@/components/app/panel-skeleton";
import { redirect } from "next/navigation";

import Grid from "@mui/material/Grid";
import { KpiWidget } from "@/components/app/kpi-widget";
import { EmptyState } from "@/components/app/empty-state";
import { CategoriesCard } from "@/components/app/categories-card";
import { EcommerceSalesOverview } from "@/components/app/sections/overview/e-commerce/ecommerce-sales-overview";
import { copy, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import { INTERNAL_LOGIN_PATH } from "@/lib/auth/session-cookie";
import { firstAuthRequiredError, getGrowthAssumptions, getShedWeights, listAnimalStages } from "@/lib/api/server";
import { stageNameMap, stageVocabularyLabel, type StageNameMap } from "@/lib/stage-display";
import { assumptionValue, DEFAULT_SALE_READY_THRESHOLD_KG } from "@/features/weighing";
import { istDayPlus, todayIso } from "@/lib/format";
import { getSalesOverview } from "@/lib/api/procurement-server";
import type { SalesOverview } from "@/lib/api/procurement";
import { boundedInt, one, type RouteSearchParams } from "@/lib/search-params";
import { countKey, inr, num } from "./sales-format";
import { SalesFarmToggle, SalesPageHeader, readSalesParkScope } from "./sales-chrome";
import { Over35Kpi } from "./over35-kpi";
import { OVER35_MAX_TOLERANCE_G, OVER35_WINDOW_DAYS } from "./over35-window";
import { salesErrorText } from "./sales-error";
import Alert from "@mui/material/Alert";
import Box from "@mui/material/Box";
import Typography from "@mui/material/Typography";

const PAGE_PATH = "/sales/farm-value";

/**
 * The Over 35 kg card's figure (maintainer request 2026-09-03). `count` is null when the read
 * failed or the caller may not read weighing; the card then shows the backend's reason rather
 * than a zero that would claim no kid is ready for sale.
 */
type Over35Card = {
  enabled: boolean;
  disabledReason: string;
  count: number | null;
  from: string;
  to: string;
  toleranceG: number;
  thresholdKg: number;
  /** The sale line before tolerance -- the tenant's assumption -- for the "Over N kg" label. */
  lineKg: number;
  /** The page's park, so the card re-counts for the same scope the rest of the page shows. */
  parkId: string;
};

/**
 * The buckets whose card shows its animals by sex (maintainer request 2026-09-11: "male, female,
 * missing ... for K0/K2/K3 or fattening ... no need for the other two"). Adult females and adult
 * males are ONE sex by construction, so a split there would only restate the label.
 */
const SEX_SPLIT_BUCKETS = new Set(["fattening", "K0", "K1", "K2", "K3"]);
/** Template progress-bar colours for the by-category rows, in the locked palette order. */
const BUCKET_BAR = ["primary", "info", "secondary", "warning"] as const;

/** "not valued" (contract copy) as a line opener: first letter up, nothing else touched. */
function sentenceCase(text: string): string {
  return text.length === 0 ? text : text[0].toUpperCase() + text.slice(1);
}

function farmValuationNotValuedLabel(overview: SalesOverview, pageContract: AdminUiPageContract): string {
  const notValued = overview.farm_valuation.not_valued ?? [];
  const total = overview.farm_valuation.excluded_animals;
  if (total <= 0) return "";
  if (notValued.length === 1) {
    const item = notValued[0];
    return `${num(item.count)} ${item.label} ${copy(pageContract, "value.not_valued")}`;
  }
  if (notValued.length > 1) {
    return `${num(total)} ${copy(pageContract, "value.not_valued")}`;
  }
  return `${num(total)} ${copy(pageContract, "value.excluded_animals")}`;
}

/**
 * Farm value — what is standing on the farm right now (split out of the Sales board, maintainer
 * decision 2026-09-11; the block itself is the 2026-09-10 decision): the live-herd valuation at
 * Sales target rates, total meat, the Over 35 kg sale-ready count with its error margin, and the
 * by-category breakdown that divides the same total. Verbatim the board's block, on its own page.
 *
 * READ-ONLY BY CONTRACT: the page declares one read card and no write control.
 */
function FarmValueSections({
  overview,
  pageContract,
  over35,
  stageNames,
}: {
  overview: SalesOverview;
  pageContract: AdminUiPageContract;
  over35: Over35Card;
  /** Tenant stage vocabulary: the by-category buckets keyed by stage code (K0…K3) show its words. */
  stageNames: StageNameMap;
}) {
  const none = copy(pageContract, "value.none");
  const kgSuffix = copy(pageContract, "value.kg_suffix");
  const notValuedLabel = farmValuationNotValuedLabel(overview, pageContract);
  return (
    <Grid container spacing={3}>
      {/* FARM VALUE — what is standing on the farm right now (maintainer decision 2026-09-10).
          Its own block, followed by the category breakdown that divides the same total. It used
          to open a strip that ran straight into the sold tiles, putting the herd valuation next to
          the sales revenue — two figures about different herds. Template CourseWidgetSummary
          cards (figure, title, tone icon; no series exists for a valuation). */}
      <Grid size={12}>
        <Grid container spacing={3} component="section" aria-label={copy(pageContract, "section.farm_value.aria")}>
          <Grid size={{ xs: 12, md: 4 }}>
            <KpiWidget color="primary" title={copy(pageContract, "kpi.farm_value")}
 caption={`₹`} total={overview.farm_valuation.total_value_rupees} icon="certificates" />
          </Grid>
          <Grid size={{ xs: 12, md: 4 }}>
            <KpiWidget color="info" title={copy(pageContract, "kpi.total_meat")}
 caption={`${kgSuffix}`} total={overview.farm_valuation.total_meat_kg} />
          </Grid>
          <Grid size={{ xs: 12, md: 4 }}>
            {/* Over 35 kg belongs with the valuation, not the ledger (maintainer decision
                2026-09-10): animals STANDING ON THE FARM that have reached sale weight. Gated by
                the page contract: a role that may not read weights sees the backend's reason,
                never a zero. */}
            <Over35Kpi
              // Re-mounted when the page's own park or margin changes underneath it.
              key={`${over35.parkId}|${over35.toleranceG}`}
              parkId={over35.parkId}
              enabled={over35.enabled}
              disabledReason={over35.disabledReason}
              initialCount={over35.count}
              initialToleranceG={over35.toleranceG}
              lineKg={over35.lineKg}
              maxG={OVER35_MAX_TOLERANCE_G}
              labels={{
                title: copy(pageContract, "kpi.over35"),
                sub: copy(pageContract, "kpi.over35.sub"),
                none: copy(pageContract, "kpi.over35.none"),
                noneValue: none,
                tolerance: copy(pageContract, "kpi.over35.tolerance"),
                apply: copy(pageContract, "kpi.over35.apply"),
                failed: copy(pageContract, "error.load"),
              }}
            />
          </Grid>
        </Grid>
      </Grid>

      {/* By category: the template Banking expenses-categories card (share of value per bucket)
          beside the Ecommerce sales-overview rows that carry each bucket's value, share and meta
          line (sex split where the bucket has one, else kg · animals). Buckets keyed by a stage
          code show the tenant's word for it (K0 → Newborn); a bucket worth nothing is named once
          under the rows instead of holding an empty bar. The weighed-count behind the fattening
          average is deliberately not printed (maintainer instruction 2026-09-11). */}
      {(() => {
        const total = overview.farm_valuation.total_value_rupees;
        const buckets = overview.farm_valuation.buckets.map((bucket) => ({
          ...bucket,
          display: stageVocabularyLabel(bucket.label, stageNames),
          meta: SEX_SPLIT_BUCKETS.has(bucket.bucket)
            ? `${num(bucket.male_count)} ${copy(pageContract, "value.sex.male")} · ${num(bucket.female_count)} ${copy(pageContract, "value.sex.female")} · ${num(bucket.meat_kg, 1)} ${kgSuffix}`
            : `${num(bucket.meat_kg, 1)} ${kgSuffix} · ${num(bucket.animal_count)} ${copy(pageContract, countKey(bucket.animal_count, "value.live_animal", "value.live_animals"))}`,
        }));
        const valued = buckets.filter((bucket) => bucket.value_rupees > 0);
        const unvalued = buckets.filter((bucket) => !(bucket.value_rupees > 0));
        const animalsLine = `${num(overview.farm_valuation.total_animals)} ${copy(pageContract, countKey(overview.farm_valuation.total_animals, "value.live_animal", "value.live_animals"))}`;
        return (
          <>
            <Grid size={{ xs: 12, md: 6, lg: 5 }}>
              <CategoriesCard
                component="section"
                aria-label={copy(pageContract, "section.farm_value.aria")}
                title={copy(pageContract, "section.farm_value.title")}
                chart={{
                  series: valued.map((bucket) => ({ label: bucket.display, value: bucket.value_rupees, display: inr(bucket.value_rupees) })),
                  // The rings carry no raw rupee ticks (400000...): the legend names every figure.
                  options: { yaxis: { labels: { show: false } } },
                }}
                footer={[
                  { label: copy(pageContract, "value.valued_animals"), value: num(overview.farm_valuation.valued_animals) },
                  { label: copy(pageContract, "kpi.farm_value"), value: inr(total) },
                ]}
                // Our category names are long ("Fattening animals · Female"); the template legend's
                // two 1fr columns let one run into the next. minmax(0, 1fr) lets a name wrap in its
                // column. Styled from here: components/minimal stays verbatim.
                sx={{ height: 1, "& .minimal__chart__legends__root": { gridTemplateColumns: "repeat(2, minmax(0, 1fr))" }, "& .minimal__chart__legends__root > *": { minWidth: 0 } }}
              />
            </Grid>
            <Grid size={{ xs: 12, md: 6, lg: 7 }}>
              <EcommerceSalesOverview
                component="section"
                aria-label={copy(pageContract, "section.farm_value.breakdown")}
                title={copy(pageContract, "section.farm_value.breakdown")}
                subheader={[animalsLine, notValuedLabel].filter(Boolean).join(" · ")}
                data={valued.map((bucket, i) => ({
                  label: bucket.display,
                  value: total > 0 ? (bucket.value_rupees / total) * 100 : 0,
                  display: inr(bucket.value_rupees),
                  caption: bucket.meta,
                  color: BUCKET_BAR[i % BUCKET_BAR.length],
                }))}
                sx={{ height: 1 }}
              >
                {valued.length === 0 ? <EmptyState title={none} /> : null}
                {unvalued.length > 0 ? (
                  <Typography variant="body2" color="text.secondary" component="p" sx={{ m: 0, pt: 2, borderTop: "1px dashed", borderColor: "divider" }}>
                    <Box component="b" sx={{ color: "text.primary" }}>{sentenceCase(copy(pageContract, "value.not_valued"))}</Box>
                    {" · "}
                    {unvalued.map((bucket) => bucket.display).join(" · ")}
                  </Typography>
                ) : null}
              </EcommerceSalesOverview>
            </Grid>
          </>
        );
      })()}
    </Grid>
  );
}

export async function SalesFarmValuePage({
  searchParams,
  pageContract,
}: {
  searchParams: RouteSearchParams;
  pageContract: AdminUiPageContract;
}) {
  const sp = searchParams;

  // Park scope: the SHELL's `park` (one filter across every Sales page). The valuation is keyed by
  // the deal farm code, the Over 35 kg count by the park itself.
  const { parkId, farm, parks } = await readSalesParkScope(sp, pageContract, PAGE_PATH);

  return (
    <div className="screen on">
      <SalesPageHeader pageContract={pageContract} />

      <SalesFarmToggle
        pageContract={pageContract}
        pagePath={PAGE_PATH}
        searchParams={sp}
        parkId={parkId}
        parks={parks}
      />

      {/* The valuation streams (guard: url-keyed-panel): a farm click swaps it to its skeleton at
          once; header and farm chips stay on screen. */}
      <UrlSuspense searchParams={sp} watch={VALUE_WATCH} fallback={<PanelSkeleton kpis={4} charts={2} />}>
        <FarmValuePanel sp={sp} pageContract={pageContract} parkId={parkId} farm={farm} />
      </UrlSuspense>
    </div>
  );
}

/** The params the valuation and the Over 35 kg count take. */
const VALUE_WATCH = ["park", "farm", "sale_ready_tolerance_g"] as const;

async function FarmValuePanel({ sp, pageContract, parkId, farm }: { sp: RouteSearchParams; pageContract: AdminUiPageContract; parkId: string; farm: string }) {
  // The Over 35 kg card reads weighing only when the contract enables it: fetch = render, and a
  // role the weighing endpoint would refuse is never asked to make that call.
  const over35Control = pageContract.controls.find((item) => item.id === "weights_over_35_card");
  const over35Enabled = over35Control?.enabled ?? false;
  const over35To = todayIso();
  const over35From = istDayPlus(over35To, -OVER35_WINDOW_DAYS);
  const over35ToleranceG = boundedInt(one(sp, "sale_ready_tolerance_g"), 0, 0, OVER35_MAX_TOLERANCE_G);
  // The sale-ready line itself is the tenant's assumption (maintainer decision 2026-09-19), read
  // once and handed to the weighing count -- weighing does not read the assumptions table. A
  // failed read (other than auth) is the CARD's failure: the count is not taken at the constants'
  // line and shown as valid; the card carries the error and no number (PR #320 review).
  // The valuation does not depend on the sale line, so it is ASKED FOR FIRST and runs beside the
  // assumptions -> weighing chain rather than after it (flicker fix, 2026-09-25).
  const overviewPromise = getSalesOverview({ farm });
  const assumptions = over35Enabled ? await getGrowthAssumptions() : null;
  if (assumptions && firstAuthRequiredError(assumptions)) redirect(INTERNAL_LOGIN_PATH);
  const assumptionsFailed = assumptions != null && !assumptions.ok;
  const saleThresholdKg =
    (assumptions?.ok ? assumptionValue(assumptions.data.values, "sale_ready_threshold_kg") : null) ?? DEFAULT_SALE_READY_THRESHOLD_KG;
  const saleLowerKg = (assumptions?.ok ? assumptionValue(assumptions.data.values, "sale_ready_lower_kg") : null) ?? undefined;
  const over35ThresholdKg = Math.max(0, saleThresholdKg - over35ToleranceG / 1000);
  const over35Params = {
    from: over35From,
    to: over35To,
    sale_threshold_tolerance_g: String(over35ToleranceG),
    sale_threshold_kg: saleThresholdKg,
    sale_lower_kg: saleLowerKg,
  };
  // The weighing count is read for the selected park directly: the page's scope IS the park id
  // the weighing read takes, so there is no all-parks read to throw away.
  const [overviewResult, weightsResult, stageResult] = await Promise.all([
    overviewPromise,
    over35Enabled && !assumptionsFailed ? getShedWeights({ ...over35Params, ...(parkId ? { park_id: parkId } : {}) }) : Promise.resolve(null),
    // Tenant stage vocabulary, so a bucket keyed by a stage code (K0…K3) shows the tenant's word
    // for it (lib/stage-display). Values stay the code; only the label changes.
    listAnimalStages(),
  ]);
  if (firstAuthRequiredError(overviewResult, stageResult)) redirect(INTERNAL_LOGIN_PATH);
  const stageNames = stageNameMap(stageResult.ok ? stageResult.data.items : undefined);
  const over35Count: number | null = weightsResult?.ok ? weightsResult.data.summary.at_or_above_35kg : null;
  const over35: Over35Card = {
    enabled: over35Enabled && !assumptionsFailed,
    disabledReason: assumptionsFailed
      ? copy(pageContract, "error.load")
      : (over35Control?.disabled_reason ?? ""),
    count: over35Count,
    from: over35From,
    to: over35To,
    toleranceG: over35ToleranceG,
    thresholdKg: over35ThresholdKg,
    lineKg: saleThresholdKg,
    parkId,
  };
  const overview: SalesOverview | null = overviewResult.ok ? overviewResult.data : null;

  return (
    <>
      {!overviewResult.ok ? (
        <Alert severity="error" sx={{ mb: 3 }}>
          {salesErrorText(overviewResult.error, copy(pageContract, "error.load"))}
        </Alert>
      ) : null}
      {assumptions && !assumptions.ok ? (
        <Alert severity="error" sx={{ mb: 3 }}>
          {salesErrorText(assumptions.error, copy(pageContract, "error.load"))}
        </Alert>
      ) : null}

      {overview ? <FarmValueSections overview={overview} pageContract={pageContract} over35={over35} stageNames={stageNames} /> : null}
    </>
  );
}
