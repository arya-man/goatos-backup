import Link from "next/link";
import { Activity, FileSearch, Search } from "lucide-react";
import { EmptyPanel, ErrorPanel, PageHeader, Panel, StatPill } from "@/components/admin-primitives";
import { getAdminRuntimeStatus, getIdentityCounts, listConflicts, searchGoats } from "@/lib/api/server";
import { dash } from "@/lib/format";

export async function OverviewPage() {
  const [counts, conflicts, herd] = await Promise.all([
    getIdentityCounts({ grain: "tenant_lifecycle", limit: 5 }),
    listConflicts({ limit: 5, state: "open" }),
    searchGoats({ limit: 5 }),
  ]);
  const runtime = getAdminRuntimeStatus();

  return (
    <>
      <PageHeader
        eyebrow="Overview"
        title="Mesha Herd Operations"
        description="Read-only Phase 1 admin surface for goat search, passport review, identity counters, and data quality queues."
      />
      <div className="mb-5 grid gap-3 md:grid-cols-3">
        <StatPill label="API base" value={runtime.baseUrl} />
        <StatPill label="Bearer token" value={runtime.hasBearerToken ? "server configured" : "missing"} tone={runtime.hasBearerToken ? "good" : "warn"} />
        <StatPill label="Tenant ID" value={runtime.hasTenantId ? "server configured" : "missing"} tone={runtime.hasTenantId ? "good" : "warn"} />
      </div>
      <div className="grid gap-5 xl:grid-cols-3">
        <Panel
          title="Herd Search"
          description="First bounded page from GET /goats/search."
          action={<Nav href="/herd" label="Open" icon={<Search className="h-4 w-4" aria-hidden="true" />} />}
        >
          {!herd.ok ? (
            <ErrorPanel error={herd.error} />
          ) : herd.data.items.length === 0 ? (
            <EmptyPanel message="No goats returned on the first page." />
          ) : (
            <div className="space-y-2">
              {herd.data.items.map((goat) => (
                <Link key={goat.goat_id} href={`/goats/${goat.goat_id}`} className="block rounded-md border border-[#293241] bg-[#10141b] p-3 hover:border-[#14f1d9]/60">
                  <div className="font-semibold text-white">{goat.display_id}</div>
                  <div className="mt-1 text-sm text-[#93a4b8]">{dash(goat.location_path.display)}</div>
                </Link>
              ))}
            </div>
          )}
        </Panel>
        <Panel
          title="Identity Counts"
          description="Tenant lifecycle counter sample."
          action={<Nav href="/counts" label="Open" icon={<Activity className="h-4 w-4" aria-hidden="true" />} />}
        >
          {!counts.ok ? (
            <ErrorPanel error={counts.error} />
          ) : counts.data.items.length === 0 ? (
            <EmptyPanel message="No counter rows returned." />
          ) : (
            <div className="space-y-2">
              {counts.data.items.map((item, index) => (
                <div key={`${item.counter_grain}-${index}`} className="rounded-md border border-[#293241] bg-[#10141b] p-3">
                  <div className="text-xl font-semibold text-white">{item.count_value.toLocaleString("en-IN")}</div>
                  <div className="mt-1 text-sm text-[#93a4b8]">{dash(item.dimensions.lifecycle_status)}</div>
                </div>
              ))}
            </div>
          )}
        </Panel>
        <Panel
          title="Data Quality"
          description="Open conflict queue sample."
          action={<Nav href="/data-quality" label="Open" icon={<FileSearch className="h-4 w-4" aria-hidden="true" />} />}
        >
          {!conflicts.ok ? (
            <ErrorPanel error={conflicts.error} />
          ) : conflicts.data.items.length === 0 ? (
            <EmptyPanel message="No open conflicts returned." />
          ) : (
            <div className="space-y-2">
              {conflicts.data.items.map((conflict) => (
                <div key={conflict.conflict_id} className="rounded-md border border-[#293241] bg-[#10141b] p-3">
                  <div className="font-semibold text-white">{conflict.conflict_type}</div>
                  <div className="mt-1 text-sm text-[#93a4b8]">{conflict.goat_count} goats · {conflict.source_record_count} sources</div>
                </div>
              ))}
            </div>
          )}
        </Panel>
      </div>
      <div className="mt-5">
        <Panel title="Honest placeholders" description="These screens need backend read endpoints or canonical apply semantics before they become live.">
          <div className="grid gap-3 md:grid-cols-2 xl:grid-cols-4">
            {["Import run rows", "Correction request admin", "Admin goat create/update", "Goat timeline"].map((item) => (
              <div key={item} className="rounded-md border border-dashed border-[#334155] bg-[#10141b] p-3 text-sm text-[#c7d1dc]">
                {item}
              </div>
            ))}
          </div>
        </Panel>
      </div>
    </>
  );
}

function Nav({ href, label, icon }: { href: string; label: string; icon: React.ReactNode }) {
  return (
    <Link href={href} className="inline-flex h-8 items-center gap-2 rounded-md border border-[#334155] px-3 text-sm text-[#f8fafc] hover:bg-[#202631]">
      {icon}
      {label}
    </Link>
  );
}
