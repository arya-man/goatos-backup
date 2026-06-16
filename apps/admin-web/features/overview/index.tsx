import Link from "next/link";
import { Activity, AlertTriangle, ClipboardList, DatabaseZap, FileSearch, Search } from "lucide-react";
import { redirect } from "next/navigation";
import { KPICard } from "@/components/charts/kpi-card";
import { EmptyPanel, ErrorPanel, PageHeader, Panel, StatPill } from "@/components/admin-primitives";
import {
  firstAuthRequiredError,
  getAdminRuntimeStatus,
  getIdentityCounts,
  getImportRun,
  listCandidates,
  listConflicts,
  listImportRuns,
  searchGoats,
} from "@/lib/api/server";
import { dash, shortId } from "@/lib/format";

export async function OverviewPage() {
  const runtime = getAdminRuntimeStatus();
  const [counts, conflicts, candidates, herd, recentRuns] = await Promise.all([
    getIdentityCounts({ grain: "tenant_lifecycle", limit: 20 }),
    listConflicts({ limit: 5, state: "open" }),
    listCandidates({ limit: 5 }),
    searchGoats({ limit: 5 }),
    runtime.importRunId ? Promise.resolve(null) : listImportRuns({ limit: 1 }),
  ]);
  const authError = firstAuthRequiredError(counts, conflicts, candidates, herd, recentRuns);
  if (authError) {
    redirect("/login");
  }

  const selectedImportRunId = runtime.importRunId ?? (recentRuns?.ok ? recentRuns.data.items[0]?.import_run_id ?? null : null);
  const importRun = selectedImportRunId ? await getImportRun(selectedImportRunId) : null;
  const importRunAuthError = firstAuthRequiredError(importRun);
  if (importRunAuthError) {
    redirect("/login");
  }

  const activeGoats = counts.ok ? tenantLifecycleCount(counts.data.items, "alive") : null;
  const reviewRows = importRun?.ok ? importRun.data.import_run.summary.rows_needing_review : null;

  return (
    <>
      <PageHeader
        eyebrow="CEO Dashboard"
        title="Mesha Herd Operations"
        description="Live command center for herd passports, identity counts, import review, and data-quality queues."
      />

      <div className="grid gap-4 sm:grid-cols-2 xl:grid-cols-4">
        <KPICard
          label="Active goats"
          value={activeGoats === null ? "Not tracked" : activeGoats.toLocaleString("en-IN")}
          subtitle="tenant_lifecycle alive · open Herd Search"
          icon={<Activity size={18} />}
          delay={0}
          variant={activeGoats === null ? "amber" : "positive"}
          href="/herd"
          navLabel="Open Herd Search"
        />
        <KPICard
          label="Open conflicts"
          value={conflicts.ok ? conflicts.data.items.length.toLocaleString("en-IN") : "Not tracked"}
          subtitle="open Data Quality conflicts"
          icon={<AlertTriangle size={18} />}
          delay={1}
          variant={conflicts.ok && conflicts.data.items.length > 0 ? "amber" : "default"}
          href="/data-quality?state=open"
          navLabel="Open Data Quality conflicts"
        />
        <KPICard
          label="Match candidates"
          value={candidates.ok ? candidates.data.items.length.toLocaleString("en-IN") : "Not tracked"}
          subtitle="open Data Quality candidates"
          icon={<FileSearch size={18} />}
          delay={2}
          href="/data-quality"
          navLabel="Open Data Quality candidates"
        />
        <KPICard
          label="Import review"
          value={reviewRows === null ? "No run" : reviewRows.toLocaleString("en-IN")}
          subtitle={selectedImportRunId ? `run ${shortId(selectedImportRunId)} · open latest` : "No import runs found"}
          icon={<DatabaseZap size={18} />}
          delay={3}
          variant={reviewRows === null ? "amber" : "default"}
          href={selectedImportRunId ? `/import-review?import_run_id=${encodeURIComponent(selectedImportRunId)}` : "/import-review"}
          navLabel="Open latest Import Review run"
        />
      </div>

      <div className="mt-6 grid gap-5 xl:grid-cols-[1.05fr_0.95fr]">
        <Panel
          title="Herd Passport Queue"
          description="First page from the live herd search."
          action={<Nav href="/herd" label="Open Herd Search" icon={<Search className="h-4 w-4" aria-hidden="true" />} />}
        >
          {!herd.ok ? (
            <ErrorPanel error={herd.error} />
          ) : herd.data.items.length === 0 ? (
            <EmptyPanel message="No goats returned on the first page." />
          ) : (
            <div className="grid gap-3 lg:grid-cols-2">
              {herd.data.items.map((goat) => (
                <Link
                  key={goat.goat_id}
                  href={`/goats/${goat.goat_id}`}
                  className="rounded-lg border border-[#334155] bg-[#11151C] p-3 transition-colors hover:border-[#14F1D9]/70"
                >
                  <div className="flex items-start justify-between gap-3">
                    <div>
                      <div className="font-semibold text-white">{goat.display_id}</div>
                      <div className="mt-1 text-xs text-[#8899AA]">{dash(goat.location_path.display)}</div>
                    </div>
                    <span className="rounded border border-[#334155] px-2 py-1 text-[10px] uppercase tracking-wider text-[#B0BEC5]">
                      {goat.identity_state}
                    </span>
                  </div>
                  <div className="mt-3 grid gap-1 text-xs text-[#B0BEC5] sm:grid-cols-2">
                    <span>RFID {dash(goat.rfid)}</span>
                    <span>Old tag {dash(goat.primary_old_tag)}</span>
                    <span>{dash(goat.breed)}</span>
                    <span>{dash(goat.lifecycle_status)}</span>
                  </div>
                </Link>
              ))}
            </div>
          )}
        </Panel>

        <Panel
          title="Import Review"
          description="Latest import run summary from the backend."
          action={
            selectedImportRunId ? (
              <Nav href={`/import-review?import_run_id=${encodeURIComponent(selectedImportRunId)}`} label="Open Run" icon={<DatabaseZap className="h-4 w-4" aria-hidden="true" />} />
            ) : null
          }
        >
          {recentRuns && !recentRuns.ok ? (
            <ErrorPanel error={recentRuns.error} />
          ) : !selectedImportRunId ? (
            <EmptyPanel message="No import runs found. Run the local RFID import/reconcile flow first." />
          ) : importRun && !importRun.ok ? (
            <ErrorPanel error={importRun.error} />
          ) : importRun?.ok ? (
            <div className="space-y-3">
              <div className="grid gap-3 sm:grid-cols-3">
                <StatPill label="Created" value={importRun.data.import_run.summary.goats_created.toLocaleString("en-IN")} tone="good" />
                <StatPill label="Review" value={importRun.data.import_run.summary.rows_needing_review.toLocaleString("en-IN")} tone="warn" />
                <StatPill label="Errors" value={importRun.data.import_run.summary.error_count.toLocaleString("en-IN")} />
              </div>
              <div className="rounded-lg border border-[#334155] bg-[#11151C] p-3 text-sm text-[#B0BEC5]">
                <span className="text-[#14F1D9]">Status</span> {importRun.data.import_run.status} · {importRun.data.import_run.source_dataset}
              </div>
            </div>
          ) : null}
        </Panel>
      </div>

      <div className="mt-6 grid gap-5 xl:grid-cols-2">
        <Panel
          title="Data Quality"
          description="Live conflict and candidate read queues. Empty queues are shown honestly."
          action={<Nav href="/data-quality?state=open" label="Open Data Quality" icon={<FileSearch className="h-4 w-4" aria-hidden="true" />} />}
        >
          {!conflicts.ok ? (
            <ErrorPanel error={conflicts.error} />
          ) : conflicts.data.items.length === 0 ? (
            <EmptyPanel message="No open conflicts returned." />
          ) : (
            <div className="space-y-2">
              {conflicts.data.items.map((conflict) => (
                <div key={conflict.conflict_id} className="rounded-lg border border-[#334155] bg-[#11151C] p-3">
                  <div className="font-semibold text-white">{conflict.conflict_type}</div>
                  <div className="mt-1 text-sm text-[#8899AA]">{conflict.goat_count} goats · {conflict.source_record_count} sources</div>
                </div>
              ))}
            </div>
          )}
        </Panel>

        <Panel title="Coming Modules" description="Visible modules stay disabled until live reads exist; no sample numbers are shown.">
          <div className="grid gap-2 sm:grid-cols-2">
            {["Mortality", "Births", "Fattening", "Feed", "Sales", "MIS", "Infra", "Vaccination"].map((item) => (
              <div key={item} className="rounded-lg border border-dashed border-[#334155] bg-[#11151C] p-3 text-sm text-[#8899AA]">
                <ClipboardList className="mb-2 h-4 w-4 text-[#566273]" aria-hidden="true" />
                {item} · Coming soon
              </div>
            ))}
          </div>
        </Panel>
      </div>
    </>
  );
}

function tenantLifecycleCount(items: Array<{ count_value: number; dimensions: { lifecycle_status?: string | null } }>, lifecycle: string): number | null {
  const row = items.find((item) => item.dimensions.lifecycle_status === lifecycle);
  return row?.count_value ?? null;
}

function Nav({ href, label, icon }: { href: string; label: string; icon: React.ReactNode }) {
  return (
    <Link href={href} className="inline-flex h-10 items-center gap-2 whitespace-nowrap rounded-lg border border-[#334155] px-3 text-sm text-[#f8fafc] hover:bg-[#22262E]">
      {icon}
      {label}
    </Link>
  );
}
