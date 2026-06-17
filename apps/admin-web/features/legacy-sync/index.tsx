import Link from "next/link";
import { AlertTriangle, Clock3, DatabaseZap, FileSearch, History, Play, RotateCcw, XCircle } from "lucide-react";
import { redirect } from "next/navigation";
import {
  ActionNotice,
  EmptyPanel,
  ErrorPanel,
  Mono,
  PageHeader,
  Panel,
  RowsPerPageSelect,
  StatPill,
  ValueList,
} from "@/components/admin-primitives";
import { ConfirmSubmitButton } from "@/components/confirm-submit-button";
import { DialogModal } from "@/components/dialog-modal";
import { INTERNAL_LOGIN_PATH } from "@/lib/auth/session-cookie";
import { formatLabel } from "@/lib/display-utils";
import { dateTime, dash, shortId } from "@/lib/format";
import { formAction } from "@/lib/routes";
import { boundedInt, hrefWithoutAction, one, type RouteSearchParams } from "@/lib/search-params";
import {
  firstAuthRequiredError,
  getLegacySyncRun,
  getLegacySyncStatus,
  listLegacySyncRuns,
  type LegacySyncDomain,
  type LegacySyncMode,
  type LegacySyncOverallStatusResponse,
  type LegacySyncRun,
  type LegacySyncRunDetailResponse,
  type LegacySyncSource,
} from "@/lib/api/server";
import { cancelLegacySyncRunAction, createLegacySyncRunAction } from "./actions";

const modes: LegacySyncMode[] = ["dry_run"];
const domains: LegacySyncDomain[] = ["all", "identity", "lifecycle", "current_location", "active_count"];
const terminalStatuses = new Set(["completed", "failed", "blocked", "canceled"]);

export async function LegacySyncPage({ searchParams }: { searchParams: RouteSearchParams }) {
  const limit = boundedInt(one(searchParams, "limit"), 20, 1, 50);
  const syncRunId = one(searchParams, "sync_run_id");
  const returnTo = hrefWithoutAction("/legacy-sync", searchParams);
  const actionStatus = one(searchParams, "action_status");
  const actionMessage = one(searchParams, "action_message");
  const modalOpen = one(searchParams, "sync_modal") === "1";

  const [status, runs, detail] = await Promise.all([
    getLegacySyncStatus(),
    listLegacySyncRuns({ limit }),
    syncRunId ? getLegacySyncRun(syncRunId) : Promise.resolve(null),
  ]);
  const authError = firstAuthRequiredError(status, runs, detail);
  if (authError) {
    redirect(INTERNAL_LOGIN_PATH);
  }

  const sourceWarnings = status.ok ? status.data.sources.filter(sourceNeedsAttention) : [];

  return (
    <>
      <PageHeader
        eyebrow="Legacy Sync"
        title="Sync Legacy Data"
        description="Backend-owned sync status for legacy evidence flowing into Mesha passports, review queues, and counters."
        actions={
          <Link
            href="/legacy-sync?sync_modal=1"
            className="inline-flex h-10 items-center gap-2 rounded-md bg-[#14f1d9] px-3 text-sm font-semibold text-[#081015] hover:bg-[#5ff7e8]"
          >
            <RotateCcw className="h-4 w-4" aria-hidden="true" />
            Sync legacy data
          </Link>
        }
      />
      <ActionNotice status={actionStatus} message={actionMessage} />
      <DialogModal open={modalOpen} closeHref="/legacy-sync" label="Sync legacy data">
        <div className="p-5">
          <SyncLauncher status={status.ok ? status.data : null} closeHref="/legacy-sync" />
        </div>
      </DialogModal>

      <div className="grid gap-5 xl:grid-cols-2">
        <Panel title="Freshness" description="Critical sources and counter freshness determine whether the dashboard can be marked fresh. Noncritical degraded sources stay visible.">
          {!status.ok ? (
            <ErrorPanel error={status.error} />
          ) : (
            <div className="space-y-4">
              <div className="grid gap-3 sm:grid-cols-3">
                <StatPill label="overall" value={freshnessSummaryValue(status.data.overall_freshness, status.data.sources)} tone={freshnessTone(status.data.overall_freshness)} />
                <StatPill
                  label="critical sources"
                  value={freshnessSummaryValue(
                    status.data.critical_freshness,
                    status.data.sources.filter((source) => source.criticality === "critical"),
                  )}
                  tone={freshnessTone(status.data.critical_freshness)}
                />
                <StatPill label="counters" value={formatLabel(status.data.counter_freshness.freshness_status)} tone={freshnessTone(status.data.counter_freshness.freshness_status)} />
              </div>
              <ValueList
                values={[
                  ["source watermarks", sourceWatermarkSummary(status.data.sources)],
                  ["counter status", status.data.counter_freshness.status_reason],
                  ["counter updated", dateTime(status.data.counter_freshness.updated_at)],
                  ["rebuild required", status.data.counter_freshness.rebuild_required ? "Yes" : "No"],
                ]}
              />
            </div>
          )}
        </Panel>

        <Panel title="Source Readiness" description="Registered sources stay Not Synced until the backend records their first successful watermark. Unregistered observed sources stay visible for catalog review.">
          {!status.ok ? (
            <ErrorPanel error={status.error} />
          ) : sourceWarnings.length === 0 ? (
            <EmptyPanel message="All registered sources have fresh watermarks." />
          ) : (
            <div className="grid gap-3 lg:grid-cols-2">
              {sourceWarnings.map((source) => (
                <SourceCard key={source.source_id} source={source} />
              ))}
            </div>
          )}
        </Panel>
      </div>

      <div className="mt-5 grid gap-5 xl:grid-cols-2">
        <Panel
          title="Recent Runs"
          description="Progress and final status survive refresh because run state is stored in the backend."
          action={
            <form action={formAction("/legacy-sync")} className="w-40">
              <RowsPerPageSelect defaultValue={String(limit)} options={[10, 20, 50]} />
            </form>
          }
        >
          {!runs.ok ? (
            <ErrorPanel error={runs.error} />
          ) : runs.data.items.length === 0 ? (
            <EmptyPanel message="No Legacy Sync runs yet." />
          ) : (
            <div className="space-y-3">
              {runs.data.items.map((run) => (
                <RunCard key={run.sync_run_id} run={run} selected={run.sync_run_id === syncRunId} />
              ))}
            </div>
          )}
        </Panel>

        <Panel title="Run Details" description="Step timeline, source windows, counters, and correction provenance for the selected run.">
          {detail === null ? (
            <EmptyPanel message="Select a run to inspect progress and the Source & Correction Log." />
          ) : !detail.ok ? (
            <ErrorPanel error={detail.error} />
          ) : (
            <RunDetails detail={detail.data} returnTo={returnTo} />
          )}
        </Panel>
      </div>
    </>
  );
}

function SyncLauncher({ status, closeHref }: { status: LegacySyncOverallStatusResponse | null; closeHref: string }) {
  const attentionSources = status?.sources.filter(sourceNeedsAttention) ?? [];
  const firstWatermarkSources = attentionSources.filter(isAwaitingFirstWatermark);
  const warningSources = attentionSources.filter((source) => !isAwaitingFirstWatermark(source));
  return (
    <div className="space-y-4">
      <div className="grid gap-3 sm:grid-cols-3">
        <MiniStatus label="overall" value={status?.overall_freshness ?? "unknown"} sources={status?.sources ?? []} />
        <MiniStatus label="critical" value={status?.critical_freshness ?? "unknown"} sources={status?.sources.filter((source) => source.criticality === "critical") ?? []} />
        <MiniStatus label="counters" value={status?.counter_freshness.freshness_status ?? "unknown"} />
      </div>
      {firstWatermarkSources.length > 0 ? (
        <div className="rounded-md border border-[#334155] bg-[#10141b] p-3 text-sm text-[#c7d1dc]">
          <div className="font-semibold text-white">
            {firstWatermarkSources.length} registered source{firstWatermarkSources.length === 1 ? "" : "s"} awaiting first watermark
          </div>
          <p className="mt-1 text-[#93a4b8]">Dry runs record a plan only; they will not clear this state until the executor writes successful source watermarks.</p>
        </div>
      ) : null}
      {warningSources.length > 0 ? (
        <div className="rounded-md border border-[#a16207] bg-[#1f1a0d] p-3 text-sm text-[#fde68a]">
          <div className="flex items-start gap-2 font-semibold">
            <AlertTriangle className="mt-0.5 h-4 w-4 shrink-0" aria-hidden="true" />
            {warningSources.length} source warning{warningSources.length === 1 ? "" : "s"}
          </div>
          <div className="mt-2 grid gap-2">
            {warningSources.slice(0, 4).map((source) => (
              <div key={source.source_id} className="text-xs">
                {source.source_name}: {sourceStatusReason(source)}
              </div>
            ))}
          </div>
        </div>
      ) : null}
      <form action={createLegacySyncRunAction} className="space-y-4">
        <input type="hidden" name="return_to" value="/legacy-sync" />
        <div className="grid gap-3 sm:grid-cols-2">
          <Select name="domain" label="Domain" defaultValue="all" options={domains} />
          <Select name="mode" label="Mode" defaultValue="dry_run" options={modes} />
        </div>
        <div className="grid gap-3 sm:grid-cols-2">
          <DateTimeField name="source_window_start" label="Window start" />
          <DateTimeField name="source_window_end" label="Window end" />
        </div>
        <div className="rounded-md border border-[#334155] bg-[#10141b] p-3 text-sm text-[#c7d1dc]">
          <div className="font-semibold text-white">Estimate</div>
          <p className="mt-1 text-[#93a4b8]">
            The backend records a bounded dry-run plan with no mutation. Execute and nightly modes stay blocked in v1 until the backend executor is configured.
          </p>
        </div>
        <div className="flex flex-wrap items-center justify-end gap-2">
          <Link href={closeHref} className="inline-flex h-10 items-center gap-2 rounded-md border border-[#334155] px-3 text-sm font-semibold text-[#c7d1dc] hover:text-white">
            <XCircle className="h-4 w-4" aria-hidden="true" />
            Close
          </Link>
          <ConfirmSubmitButton
            message="Start Legacy Sync?"
            className="inline-flex h-10 items-center gap-2 rounded-md bg-[#14f1d9] px-3 text-sm font-semibold text-[#081015] hover:bg-[#5ff7e8]"
          >
            <Play className="h-4 w-4" aria-hidden="true" />
            Start
          </ConfirmSubmitButton>
        </div>
      </form>
    </div>
  );
}

function RunDetails({ detail, returnTo }: { detail: LegacySyncRunDetailResponse; returnTo: string }) {
  const run = detail.run;
  const canCancel = !terminalStatuses.has(run.status);
  return (
    <div className="space-y-5">
      <div className="grid gap-3 sm:grid-cols-4">
        <StatPill label="status" value={formatLabel(run.status)} tone={runTone(run.status)} />
        <StatPill label="rows read" value={run.summary.rows_read} />
        <StatPill label="rows applied" value={run.summary.rows_applied} tone={run.summary.rows_applied > 0 ? "good" : "neutral"} />
        <StatPill label="conflicts" value={run.summary.conflicts_opened + run.summary.conflicts_refreshed} tone={run.summary.conflicts_opened + run.summary.conflicts_refreshed > 0 ? "warn" : "neutral"} />
      </div>
      {run.blocked_reason ? (
        <div className="rounded-md border border-[#7f1d1d] bg-[#1d1214] p-3 text-sm text-[#fecaca]">{run.blocked_reason}</div>
      ) : null}
      <ValueList
        values={[
          ["run", <Mono key="run">{run.sync_run_id}</Mono>],
          ["mode", formatLabel(run.mode)],
          ["domain", formatLabel(run.domain)],
          ["cold start", run.cold_start ? "Yes" : "No"],
          ["started", dateTime(run.started_at)],
          ["completed", dateTime(run.completed_at)],
          ["source window start", dateTime(run.source_window_start)],
          ["source window end", dateTime(run.source_window_end)],
          ["counter check", formatLabel(run.counter_check_status)],
          ["counters rebuilt", run.counters_rebuilt ? "Yes" : "No"],
        ]}
      />
      <div className="grid gap-3 md:grid-cols-3">
        <Link href="/import-review" className="inline-flex h-10 items-center justify-center gap-2 rounded-md border border-[#334155] px-3 text-sm font-semibold text-[#c7d1dc] hover:border-[#14f1d9]/70 hover:text-white">
          <DatabaseZap className="h-4 w-4" aria-hidden="true" />
          Import Review
        </Link>
        <Link href="/data-quality" className="inline-flex h-10 items-center justify-center gap-2 rounded-md border border-[#334155] px-3 text-sm font-semibold text-[#c7d1dc] hover:border-[#14f1d9]/70 hover:text-white">
          <FileSearch className="h-4 w-4" aria-hidden="true" />
          Data Quality
        </Link>
        <a href="#source-correction-log" className="inline-flex h-10 items-center justify-center gap-2 rounded-md border border-[#334155] px-3 text-sm font-semibold text-[#c7d1dc] hover:border-[#14f1d9]/70 hover:text-white">
          <History className="h-4 w-4" aria-hidden="true" />
          Source & Correction Log
        </a>
      </div>
      {canCancel ? (
        <form action={cancelLegacySyncRunAction} className="flex justify-end">
          <input type="hidden" name="sync_run_id" value={run.sync_run_id} />
          <input type="hidden" name="return_to" value={returnTo} />
          <ConfirmSubmitButton
            message="Cancel this Legacy Sync run?"
            className="h-10 rounded-md border border-[#7f1d1d] px-3 text-sm font-semibold text-[#fecaca] hover:bg-[#1d1214]"
          >
            Cancel run
          </ConfirmSubmitButton>
        </form>
      ) : null}
      <div>
        <h3 className="text-sm font-bold text-white">Step Timeline</h3>
        <div className="mt-3 space-y-2">
          {detail.steps.length === 0 ? (
            <EmptyPanel message="No steps recorded for this run." />
          ) : (
            detail.steps.map((step) => <StepRow key={step.sync_step_id} step={step} />)
          )}
        </div>
      </div>
      <div id="source-correction-log">
        <h3 className="text-sm font-bold text-white">Source & Correction Log</h3>
        <div className="mt-3">
          {detail.source_correction_log.length === 0 ? (
            <EmptyPanel message="No source correction log rows for this run." />
          ) : (
            <div className="space-y-2">
              {detail.source_correction_log.map((item) => (
                <div key={item.sync_run_conflict_id} className="rounded-md border border-[#293241] bg-[#10141b] p-3 text-sm">
                  <div className="flex flex-wrap items-center justify-between gap-2">
                    <div className="font-semibold text-white">{formatLabel(item.evidence_reason)}</div>
                    <span className="rounded border border-[#334155] px-2 py-1 text-xs text-[#c7d1dc]">{formatLabel(item.result)}</span>
                  </div>
                  <div className="mt-2 grid gap-2 text-xs text-[#c7d1dc] sm:grid-cols-2">
                    <span>source {item.source_id}</span>
                    <span>record {item.source_record_id}</span>
                    <span>goat {shortId(item.goat_id)}</span>
                    <span>conflict {shortId(item.conflict_id)}</span>
                    <span>old {dash(item.old_goatos_value)}</span>
                    <span>new {dash(item.new_legacy_value)}</span>
                  </div>
                </div>
              ))}
            </div>
          )}
        </div>
      </div>
    </div>
  );
}

function RunCard({ run, selected }: { run: LegacySyncRun; selected: boolean }) {
  return (
    <Link
      href={`/legacy-sync?sync_run_id=${encodeURIComponent(run.sync_run_id)}`}
      className={`block rounded-md border p-3 ${
        selected ? "border-[#14f1d9] bg-[#111923]" : "border-[#293241] bg-[#10141b] hover:border-[#14f1d9]/70 hover:bg-[#111923]"
      }`}
    >
      <div className="flex items-start justify-between gap-3">
        <div>
          <div className="flex items-center gap-2 font-semibold text-white">
            <Clock3 className="h-4 w-4 text-[#14f1d9]" aria-hidden="true" />
            {formatLabel(run.mode)} - {formatLabel(run.domain)}
          </div>
          <div className="mt-1 text-xs text-[#93a4b8]">
            {dateTime(run.started_at)} - Run {shortId(run.sync_run_id)}
          </div>
        </div>
        <span className={`rounded border px-2 py-1 text-xs font-semibold ${statusClass(run.status)}`}>{formatLabel(run.status)}</span>
      </div>
      <div className="mt-3 grid grid-cols-3 gap-2 text-xs text-[#c7d1dc]">
        <span>read {run.summary.rows_read}</span>
        <span>applied {run.summary.rows_applied}</span>
        <span>conflicts {run.summary.conflicts_opened + run.summary.conflicts_refreshed}</span>
      </div>
    </Link>
  );
}

function SourceCard({ source }: { source: LegacySyncSource }) {
  const awaitingFirstWatermark = isAwaitingFirstWatermark(source);
  return (
    <div className="rounded-md border border-[#293241] bg-[#10141b] p-3">
      <div className="flex items-start justify-between gap-3">
        <div className="min-w-0">
          <div className="font-semibold text-white">{source.source_name}</div>
          <div className="mt-1 text-xs text-[#93a4b8]">{formatLabel(source.domain)} - {formatLabel(source.criticality)}</div>
        </div>
        <span className={`shrink-0 whitespace-nowrap rounded border px-2 py-1 text-xs font-semibold ${awaitingFirstWatermark ? "border-[#334155] text-[#c7d1dc]" : freshnessClass(source.freshness_status)}`}>
          {sourceStatusLabel(source)}
        </span>
      </div>
      <div className="mt-3 text-sm text-[#c7d1dc]">{sourceStatusReason(source)}</div>
      <div className="mt-2 grid gap-1 text-xs text-[#93a4b8] sm:grid-cols-2">
        <span>cadence {durationLabel(source.cadence_seconds)}</span>
        <span>green {durationLabel(source.green_within_seconds)}</span>
        <span>yellow {durationLabel(source.yellow_within_seconds)}</span>
        <span>watermark {dateTime(source.source_watermark_at)}</span>
      </div>
    </div>
  );
}

function StepRow({ step }: { step: LegacySyncRunDetailResponse["steps"][number] }) {
  return (
    <div className="rounded-md border border-[#293241] bg-[#10141b] p-3 text-sm">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <div className="font-semibold text-white">{formatLabel(step.step_name)}</div>
        <span className={`rounded border px-2 py-1 text-xs font-semibold ${statusClass(step.status)}`}>{formatLabel(step.status)}</span>
      </div>
      <div className="mt-2 grid gap-2 text-xs text-[#c7d1dc] sm:grid-cols-4">
        <span>read {step.rows_read}</span>
        <span>planned {step.rows_planned}</span>
        <span>applied {step.rows_applied}</span>
        <span>skipped {step.rows_skipped}</span>
      </div>
    </div>
  );
}

function Select({ name, label, defaultValue, options }: { name: string; label: string; defaultValue: string; options: readonly string[] }) {
  return (
    <label>
      <span className="text-xs uppercase text-[#93a4b8]">{label}</span>
      <select
        name={name}
        defaultValue={defaultValue}
        className="mt-1 h-10 w-full rounded-md border border-[#334155] bg-[#0f1115] px-3 text-sm text-white outline-none focus:border-[#14f1d9]"
      >
        {options.map((option) => (
          <option key={option} value={option}>
            {formatLabel(option)}
          </option>
        ))}
      </select>
    </label>
  );
}

function DateTimeField({ name, label }: { name: string; label: string }) {
  return (
    <label>
      <span className="text-xs uppercase text-[#93a4b8]">{label}</span>
      <input
        name={name}
        type="datetime-local"
        className="mt-1 h-10 w-full rounded-md border border-[#334155] bg-[#0f1115] px-3 text-sm text-white outline-none focus:border-[#14f1d9]"
      />
    </label>
  );
}

function MiniStatus({ label, value, sources = [] }: { label: string; value: string; sources?: LegacySyncSource[] }) {
  return (
    <div className={`rounded-md border px-3 py-2 ${freshnessClass(value)}`}>
      <div className="text-xs uppercase text-[#93a4b8]">{label}</div>
      <div className="mt-1 text-sm font-semibold">{freshnessSummaryValue(value, sources)}</div>
    </div>
  );
}

function sourceNeedsAttention(source: LegacySyncSource): boolean {
  return source.freshness_status !== "green" || source.known_degraded || source.is_unknown_source;
}

function isAwaitingFirstWatermark(source: LegacySyncSource): boolean {
  return !source.is_unknown_source && !source.known_degraded && source.freshness_status === "unknown" && !source.source_watermark_at;
}

function freshnessSummaryValue(value: string, sources: LegacySyncSource[]): string {
  if (value === "unknown" && sources.length > 0 && sources.every(isAwaitingFirstWatermark)) {
    return "Not Synced";
  }
  return formatLabel(value);
}

function sourceWatermarkSummary(sources: LegacySyncSource[]): string {
  const awaitingCount = sources.filter(isAwaitingFirstWatermark).length;
  if (awaitingCount === 0) return "all registered source watermarks recorded";
  return `${awaitingCount} registered source${awaitingCount === 1 ? "" : "s"} awaiting first successful watermark`;
}

function sourceStatusLabel(source: LegacySyncSource): string {
  if (isAwaitingFirstWatermark(source)) return "Not Synced";
  if (source.is_unknown_source) return "Unregistered";
  return formatLabel(source.freshness_status);
}

function sourceStatusReason(source: LegacySyncSource): string {
  if (isAwaitingFirstWatermark(source)) return "no successful source watermark yet";
  if (source.is_unknown_source) return "source is not registered in the backend catalog";
  return source.status_reason;
}

function freshnessTone(value: string): "neutral" | "good" | "warn" {
  return value === "green" ? "good" : value === "yellow" ? "warn" : "neutral";
}

function runTone(value: string): "neutral" | "good" | "warn" {
  return value === "completed" ? "good" : value === "blocked" || value === "failed" ? "warn" : "neutral";
}

function freshnessClass(value: string) {
  switch (value) {
    case "green":
      return "border-[#1f8f65] text-[#7dd3a7]";
    case "yellow":
      return "border-[#a16207] text-[#facc15]";
    case "red":
      return "border-[#7f1d1d] text-[#fecaca]";
    default:
      return "border-[#334155] text-[#c7d1dc]";
  }
}

function statusClass(value: string) {
  switch (value) {
    case "completed":
      return "border-[#1f8f65] text-[#7dd3a7]";
    case "blocked":
    case "failed":
      return "border-[#7f1d1d] text-[#fecaca]";
    case "running":
    case "planning":
      return "border-[#a16207] text-[#facc15]";
    default:
      return "border-[#334155] text-[#c7d1dc]";
  }
}

function durationLabel(seconds: number): string {
  if (seconds % 3600 === 0) return `${seconds / 3600}h`;
  if (seconds % 60 === 0) return `${seconds / 60}m`;
  return `${seconds}s`;
}
