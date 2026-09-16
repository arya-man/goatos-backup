import { redirect } from "next/navigation";

import { Tag } from "@/components/ui-primitives";
import { copy, optionGroup, type AdminUiPageContract } from "@/lib/admin-ui-contract";
import { INTERNAL_LOGIN_PATH } from "@/lib/auth/session-cookie";
import { firstAuthRequiredError, getShedWeights } from "@/lib/api/server";
import { istDayPlus, todayIso } from "@/lib/format";
import { getSalesOverview } from "@/lib/api/procurement-server";
import type { SalesOverview } from "@/lib/api/procurement";
import { boundedInt, one, type RouteSearchParams } from "@/lib/search-params";
import { inr, num, resolveFarm } from "./sales-format";
import { SALES_DEFAULT_FARM, SalesFarmToggle, SalesPageHeader } from "./sales-chrome";
import { SalesReadyToleranceControl } from "./sales-ready-tolerance-control";

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
  preserveQuery: [string, string][];
};

/** The nominal sale-ready lookback; backend clamps tolerance reads to reliable weighing data. */
const OVER35_WINDOW_DAYS = 42;
const OVER35_MAX_TOLERANCE_G = 1000;
/**
 * The buckets whose card shows its animals by sex (maintainer request 2026-09-11: "male, female,
 * missing ... for K0/K2/K3 or fattening ... no need for the other two"). Adult females and adult
 * males are ONE sex by construction, so a split there would only restate the label.
 */
const SEX_SPLIT_BUCKETS = new Set(["fattening", "K0", "K1", "K2", "K3"]);

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
}: {
  overview: SalesOverview;
  pageContract: AdminUiPageContract;
  over35: Over35Card;
}) {
  const none = copy(pageContract, "value.none");
  const kgSuffix = copy(pageContract, "value.kg_suffix");
  const notValuedLabel = farmValuationNotValuedLabel(overview, pageContract);
  return (
    <>
          {/* FARM VALUE — what is standing on the farm right now (maintainer decision 2026-09-10).
              Its own block, headed, and immediately followed by the category breakdown that
              divides the same total. It used to open a single strip that ran straight on into the
              sold tiles, putting the herd valuation next to the sales revenue — two figures about
              different herds, inviting a subtraction that means nothing. */}
          <section className="sales-block" aria-label={copy(pageContract, "section.farm_value.aria")}>
            <div className="sales-block-hd">
              <h3>{copy(pageContract, "section.farm_value.title")}</h3>
              <span className="muted small">{copy(pageContract, "section.farm_value.sub")}</span>
            </div>
            <div className="grid g3 kpi-row sales-kpi-row sales-farm-value-row">
              <div className="kpi">
                <div className="lab">{copy(pageContract, "kpi.farm_value")}</div>
                <div className="val">{inr(overview.farm_valuation.total_value_rupees)}</div>
                <div className="dl">{copy(pageContract, "kpi.farm_value.detail")}</div>
              </div>
              <div className="kpi">
                <div className="lab">{copy(pageContract, "kpi.total_meat")}</div>
                <div className="val">
                  {num(overview.farm_valuation.total_meat_kg, 1)} {kgSuffix}
                </div>
                <div className="dl">{copy(pageContract, "kpi.total_meat.detail")}</div>
              </div>
              {/* Over 35 kg belongs with the valuation, not the ledger (maintainer decision
                  2026-09-10). It counts animals STANDING ON THE FARM that have reached sale
                  weight — inventory ready to go, not anything that has gone. Sitting in the Sold
                  strip it read as a count of animals already sold at that weight.
                  Gated by the page contract: a role that may not read weights sees the backend's
                  reason, never a zero. */}
              <div className="kpi">
                <div className="lab">{copy(pageContract, "kpi.over35")}</div>
                <div className="val">{over35.count == null ? none : num(over35.count)}</div>
                <div className="dl">
                  {!over35.enabled
                    ? over35.disabledReason
                    : over35.count == null
                      ? copy(pageContract, "kpi.over35.none")
                      : `${copy(pageContract, "kpi.over35.sub")} · ${num(over35.thresholdKg, 1)}+`}
                </div>
                {/* The error margin tunes THIS card's figure and nothing else on the page, so it
                    sits inside the card (maintainer request 2026-09-14). As a strip under the
                    whole row it read as a page-wide control over the valuation too. */}
                {over35.enabled ? (
                  <SalesReadyToleranceControl
                    key={over35.toleranceG}
                    valueG={over35.toleranceG}
                    maxG={OVER35_MAX_TOLERANCE_G}
                    preserveQuery={over35.preserveQuery}
                    pagePath={PAGE_PATH}
                    label={copy(pageContract, "kpi.over35.tolerance")}
                    applyLabel={copy(pageContract, "kpi.over35.apply")}
                  />
                ) : null}
              </div>
            </div>
          </section>

          <section className="card sales-card" aria-label={copy(pageContract, "section.farm_value.breakdown")}>
            <div className="hd">
              <h3>{copy(pageContract, "section.farm_value.breakdown")}</h3>
              <Tag tone={overview.farm_valuation.total_value_rupees > 0 ? "info" : "mut"}>
                {num(overview.farm_valuation.valued_animals)} {copy(pageContract, "value.valued_animals")}
                {notValuedLabel ? ` · ${notValuedLabel}` : ""}
                {" · "}
                {num(overview.farm_valuation.total_animals)} {copy(pageContract, "value.live_animals")}
              </Tag>
            </div>
            <div className="grid g4">
              {overview.farm_valuation.buckets.map((bucket) => (
                <div className="kpi mini" key={bucket.bucket}>
                  <div className="lab">{bucket.label}</div>
                  {/* On top, under the label (maintainer request 2026-09-11): the bucket's animals by
                      recorded sex, male and female only ("don't show missing", same day). The two
                      are backend counts rendered verbatim; an animal with no recorded sex is in the
                      card's animal count but in neither figure here, and that gap is deliberate. */}
                  {SEX_SPLIT_BUCKETS.has(bucket.bucket) ? (
                    <div className="dl sales-sex-split" data-bucket={bucket.bucket}>
                      {num(bucket.male_count)} {copy(pageContract, "value.sex.male")} · {num(bucket.female_count)}{" "}
                      {copy(pageContract, "value.sex.female")}
                    </div>
                  ) : null}
                  <div className="val">{inr(bucket.value_rupees)}</div>
                  <div className="dl">
                    {num(bucket.meat_kg, 1)} {kgSuffix} · {num(bucket.animal_count)}{" "}
                    {copy(pageContract, "value.live_animals")}
                    {/* The "N weighed" count behind the fattening average is deliberately not
                        printed (maintainer instruction 2026-09-11). */}
                  </div>
                </div>
              ))}
            </div>
          </section>

    </>
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

  // Farm scope: validated against the SERVED option keys, never trusted raw.
  const farmOptions = optionGroup(pageContract, "sales_farms");
  const farm = resolveFarm(
    one(sp, "farm"),
    farmOptions.map((option) => option.key),
    SALES_DEFAULT_FARM,
  );

  // The Over 35 kg card reads weighing only when the contract enables it: fetch = render, and a
  // role the weighing endpoint would refuse is never asked to make that call.
  const over35Control = pageContract.controls.find((item) => item.id === "weights_over_35_card");
  const over35Enabled = over35Control?.enabled ?? false;
  const over35To = todayIso();
  const over35From = istDayPlus(over35To, -OVER35_WINDOW_DAYS);
  const over35ToleranceG = boundedInt(one(sp, "sale_ready_tolerance_g"), 0, 0, OVER35_MAX_TOLERANCE_G);
  const over35ThresholdKg = Math.max(0, 35 - over35ToleranceG / 1000);
  const over35Params = {
    from: over35From,
    to: over35To,
    sale_threshold_tolerance_g: String(over35ToleranceG),
  };
  // The page's data in ONE parallel read: the overview (whose farm_valuation is this page's
  // block) and, when enabled, the weighing count. Unscoped first: the weighing response also
  // carries the park vocabulary this page's farm code is matched against.
  const [overviewResult, weightsResult] = await Promise.all([
    getSalesOverview({ farm }),
    over35Enabled ? getShedWeights(over35Params) : Promise.resolve(null),
  ]);
  if (firstAuthRequiredError(overviewResult)) redirect(INTERNAL_LOGIN_PATH);

  // Farm scope for the card. The weighing park vocabulary names parks by their code (CBE, CPT),
  // the same code this page's farm filter carries, so the selected farm resolves to a park id
  // and the count is re-read for that park alone. An unknown farm code keeps the all-parks
  // figure rather than showing a zero for a park that was never asked about.
  let over35Count: number | null = null;
  if (weightsResult?.ok) {
    over35Count = weightsResult.data.summary.at_or_above_35kg;
    if (farm !== SALES_DEFAULT_FARM) {
      const park = weightsResult.data.parks.find((item) => item.name === farm);
      if (park) {
        const scoped = await getShedWeights({ ...over35Params, park_id: park.park_id });
        over35Count = scoped.ok ? scoped.data.summary.at_or_above_35kg : null;
      }
    }
  }
  const over35PreserveQuery = Object.entries(sp).flatMap(([key, value]) => {
    if (key === "sale_ready_tolerance_g") return [];
    const first = Array.isArray(value) ? value[0] : value;
    return first ? ([[key, first]] as [string, string][]) : [];
  });
  const over35: Over35Card = {
    enabled: over35Enabled,
    disabledReason: over35Control?.disabled_reason ?? "",
    count: over35Count,
    from: over35From,
    to: over35To,
    toleranceG: over35ToleranceG,
    thresholdKg: over35ThresholdKg,
    preserveQuery: over35PreserveQuery,
  };
  const overview: SalesOverview | null = overviewResult.ok ? overviewResult.data : null;

  return (
    <div className="screen on">
      <SalesPageHeader pageContract={pageContract} />

      {!overviewResult.ok ? (
        <div className="alert" style={{ marginBottom: 14 }}>
          <b>{overviewResult.error.code ?? overviewResult.error.kind}</b>&nbsp;
          {overviewResult.error.message || copy(pageContract, "error.load")}
        </div>
      ) : null}

      <SalesFarmToggle
        pageContract={pageContract}
        pagePath={PAGE_PATH}
        farm={farm}
        saleReadyToleranceG={over35.toleranceG}
      />

      {overview ? <FarmValueSections overview={overview} pageContract={pageContract} over35={over35} /> : null}
    </div>
  );
}
