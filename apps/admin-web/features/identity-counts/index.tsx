import { BarChart3 } from "lucide-react";
import { EmptyPanel, ErrorPanel, Mono, NextPageLink, PageHeader, Panel, StatPill, ValueList } from "@/components/admin-primitives";
import { dateTime, dash, shortId } from "@/lib/format";
import { boundedInt, hrefWithCursor, one, type RouteSearchParams } from "@/lib/search-params";
import { getIdentityCounts, type CounterGrain } from "@/lib/api/server";

const grains: CounterGrain[] = [
  "tenant_lifecycle",
  "custodian_lifecycle",
  "custodian_identity",
  "park_lifecycle",
  "shed_lifecycle",
  "breed_sex_lifecycle",
  "health_status",
  "growth_cohort",
  "management_stage",
  "reproductive_status",
];

export async function IdentityCountsPage({ searchParams }: { searchParams: RouteSearchParams }) {
  const grain = normalizeGrain(one(searchParams, "grain")) ?? "tenant_lifecycle";
  const limit = boundedInt(one(searchParams, "limit"), 50, 1, 500);
  const result = await getIdentityCounts({
    grain,
    limit,
    cursor: one(searchParams, "cursor"),
  });

  return (
    <>
      <PageHeader
        eyebrow="Analytics"
        title="Identity Counts"
        description="Materialized identity counter reads use server-side bearer auth, required tenant_id from server config, and keyset pagination."
      />
      <Panel title="Query" description="The backend rejects missing or out-of-range limits instead of clamping.">
        <form className="grid gap-3 sm:grid-cols-[minmax(220px,320px)_160px_auto]" action="/counts">
          <label>
            <span className="text-xs uppercase text-[#93a4b8]">Grain</span>
            <select
              name="grain"
              defaultValue={grain}
              className="mt-1 h-9 w-full rounded-md border border-[#334155] bg-[#0f1115] px-3 text-sm text-white outline-none focus:border-[#14f1d9]"
            >
              {grains.map((item) => (
                <option key={item} value={item}>
                  {item}
                </option>
              ))}
            </select>
          </label>
          <label>
            <span className="text-xs uppercase text-[#93a4b8]">Limit</span>
            <input
              name="limit"
              type="number"
              min="1"
              max="500"
              defaultValue={limit}
              className="mt-1 h-9 w-full rounded-md border border-[#334155] bg-[#0f1115] px-3 text-sm text-white outline-none focus:border-[#14f1d9]"
            />
          </label>
          <div className="flex items-end">
            <button className="inline-flex h-9 items-center gap-2 rounded-md bg-[#14f1d9] px-3 text-sm font-semibold text-[#081015] hover:bg-[#7ff7ea]">
              <BarChart3 className="h-4 w-4" aria-hidden="true" />
              Load
            </button>
          </div>
        </form>
      </Panel>
      <div className="mt-5">
        {!result.ok ? (
          <ErrorPanel error={result.error} />
        ) : (
          <Panel
            title={result.data.grain}
            description={`Freshness ${dateTime(result.data.freshness.as_of_recorded_at)}${result.data.freshness.warning ? ` · ${result.data.freshness.warning}` : ""}`}
            action={<StatPill label="Rows" value={result.data.items.length} tone={result.data.items.length > 0 ? "good" : "neutral"} />}
          >
            {result.data.items.length === 0 ? (
              <EmptyPanel message="No count rows returned for this grain and cursor." />
            ) : (
              <div className="space-y-3">
                {result.data.items.map((item, index) => (
                  <div key={`${item.counter_grain}-${index}-${item.updated_at}`} className="rounded-md border border-[#293241] bg-[#10141b] p-4">
                    <div className="flex flex-col gap-3 lg:flex-row lg:items-start lg:justify-between">
                      <div>
                        <div className="text-2xl font-semibold text-white">{item.count_value.toLocaleString("en-IN")}</div>
                        <div className="mt-1 text-sm text-[#93a4b8]">Updated {dateTime(item.updated_at)}</div>
                      </div>
                      <div className="flex flex-wrap gap-2 text-xs">
                        {item.is_rebuilding ? <span className="rounded border border-[#a16207] px-2 py-1 text-[#facc15]">rebuilding</span> : null}
                        {item.source_import_run_id ? <span className="rounded border border-[#334155] px-2 py-1 text-[#c7d1dc]">run {shortId(item.source_import_run_id)}</span> : null}
                      </div>
                    </div>
                    <div className="mt-4">
                      <ValueList
                        values={[
                          ["tenant", <Mono key="tenant">{item.dimensions.tenant_id}</Mono>],
                          ["custodian", dash(item.dimensions.custodian_party_id)],
                          ["farm", dash(item.dimensions.farm_id)],
                          ["park", dash(item.dimensions.park_id)],
                          ["shed", dash(item.dimensions.shed_id)],
                          ["cohort", dash(item.dimensions.cohort_id)],
                          ["lifecycle", dash(item.dimensions.lifecycle_status)],
                          ["identity", dash(item.dimensions.identity_state)],
                          ["breed", dash(item.dimensions.breed_id)],
                          ["sex", dash(item.dimensions.sex)],
                          ["health", dash(item.dimensions.health_status)],
                          ["management", dash(item.dimensions.management_stage)],
                          ["reproductive", dash(item.dimensions.reproductive_status)],
                          ["growth cohort", dash(item.dimensions.growth_cohort_tag)],
                        ]}
                      />
                    </div>
                  </div>
                ))}
                <NextPageLink href={hrefWithCursor("/counts", searchParams, result.data.next_cursor)} />
                <div className="text-xs text-[#93a4b8]">Trace {result.data.trace_id}. Has more: {result.data.has_more ? "yes" : "no"}.</div>
              </div>
            )}
          </Panel>
        )}
      </div>
    </>
  );
}

function normalizeGrain(value: string | undefined): CounterGrain | undefined {
  return grains.includes(value as CounterGrain) ? (value as CounterGrain) : undefined;
}
